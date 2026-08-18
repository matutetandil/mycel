package parser

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// What happens when an attribute holds the wrong kind of value.
//
// The parser reads most attributes with a bare cty AsString, which panics on
// anything that is not text — so `cache { ttl = 300 }`, a number where a
// duration string was wanted, took the whole binary down with "panic: not a
// string" during `mycel validate`. That is a plausible thing to write: 300 is
// what somebody means by five minutes, and the failure names neither the
// attribute nor the block.
//
// This walks the schema and puts a number in every attribute declared as text,
// one at a time, asking only that the parser not die. Refusing the value is
// the right answer; so is accepting it and reading it as "300". Crashing is
// not.
//
// It is the third parity test in this package, and the same shape as the other
// two: the schema says what the language contains, and the test asks the real
// parser whether that is true.
func TestNoAttributeCanCrashTheParser(t *testing.T) {
	for _, blk := range schema.BuiltinRootSchemas() {
		blk := blk
		t.Run(blk.Type, func(t *testing.T) {
			for _, probe := range wrongTypeProbes(blk, blk.Type) {
				probe := probe
				t.Run(probe.where, func(t *testing.T) {
					doc := probe.doc
					_, err := tryParse(t, doc)
					if err != nil && strings.HasPrefix(err.Error(), "panic:") {
						t.Errorf("%s took the parser down: %v\n\n%s", probe.where, err, doc)
					}
				})
			}
		})
	}
}

type wrongTypeProbe struct {
	where string // block.attribute, for the failure message
	doc   string
}

// wrongTypeProbes renders one document per string-typed attribute, with a
// number in place of the text.
func wrongTypeProbes(blk schema.Block, name string) []wrongTypeProbe {
	var probes []wrongTypeProbe

	labels := make([]string, blk.Labels)
	for i := range labels {
		labels[i] = name
	}

	for _, path := range stringAttrPaths(blk, nil) {
		doc := renderWithOverride(blk, labels, 0, path, "1234")
		probes = append(probes, wrongTypeProbe{
			where: strings.Join(append([]string{blk.Type}, path...), "."),
			doc:   doc,
		})
	}
	return probes
}

// stringAttrPaths lists every text attribute in a block and its children, as a
// path of block types ending in the attribute name.
func stringAttrPaths(blk schema.Block, prefix []string) [][]string {
	var paths [][]string

	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	for _, a := range attrs {
		// Only the ones that would reach AsString: a declared enum is rendered
		// as its first value and is covered by the parity test already.
		if a.Type != schema.TypeString && a.Type != schema.TypeDuration {
			continue
		}
		if len(a.Values) > 0 {
			continue
		}
		if skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		paths = append(paths, append(append([]string{}, prefix...), a.Name))
	}

	children := append([]schema.Block(nil), blk.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].Type < children[j].Type })
	for _, child := range children {
		paths = append(paths, stringAttrPaths(child, append(append([]string{}, prefix...), child.Type))...)
	}
	return paths
}

// renderWithOverride renders the block the way the parity test does, but with
// one attribute holding the given literal instead of its sample value.
func renderWithOverride(blk schema.Block, labels []string, depth int, path []string, literal string) string {
	var b strings.Builder
	b.WriteString(blockHeader(blk, labels) + " {\n")
	indent := strings.Repeat("  ", depth+1)

	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	for _, a := range attrs {
		if skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		value := sampleValue(a)
		// The override applies when this block is where the path ends.
		if len(path) == 1 && path[0] == a.Name {
			value = literal
		}
		fmt.Fprintf(&b, "%s%s = %s\n", indent, a.Name, value)
	}

	children := append([]schema.Block(nil), blk.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].Type < children[j].Type })
	for _, child := range children {
		childLabels := make([]string, child.Labels)
		for i := range childLabels {
			childLabels[i] = "example"
		}
		childPath := path
		if len(path) > 1 && path[0] == child.Type {
			childPath = path[1:]
		} else {
			childPath = nil
		}
		rendered := renderWithOverride(child, childLabels, depth+1, childPath, literal)
		for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
			b.WriteString(indent + line + "\n")
		}
	}

	b.WriteString(strings.Repeat("  ", depth) + "}\n")
	return b.String()
}

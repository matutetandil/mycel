package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// The configuration reference says of itself: "Complete HCL syntax reference for
// all Mycel block types. Every block is documented with all supported
// attributes."
//
// It has failed that twice. `accept` was not in it at all — a block a flow can
// have and the page had zero mentions of — and neither was `sequence_guard`.
// Both were found by somebody reading the page against the parser, once.

var referencePage = filepath.Join("..", "..", "docs", "reference", "configuration.md")

// headings returns every heading the page has, lower-cased.
func headings(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(referencePage)
	if err != nil {
		t.Fatalf("reading the reference: %v", err)
	}

	var out []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "#") {
			out = append(out, strings.ToLower(strings.TrimLeft(line, "# ")))
		}
	}
	return out
}

// mentionedInReference reports whether the page has a heading naming this block.
func mentionedInReference(headings []string, block string) bool {
	for _, heading := range headings {
		// "## dedupe", "### dedupe block", "## cache (named)"
		if heading == block ||
			strings.HasPrefix(heading, block+" ") ||
			strings.HasPrefix(heading, block+"_block") {
			return true
		}
	}
	return false
}

// documentedElsewhereInTheDocs lists blocks the reference legitimately leaves to
// another page, with where they are. An entry here is a decision.
var documentedElsewhereInTheDocs = map[string]string{
	"auth":     "docs/guides/auth.md, which is the length it needs to be",
	"security": "docs/guides/auth.md",
	"aspect":   "docs/core-concepts/aspects.md",
	"saga":     "docs/guides/sagas.md",
	"mock":     "docs/advanced/mocks.md",
	"mocks":    "docs/advanced/mocks.md",
}

func TestEveryBlockAFlowCanHaveIsInTheReference(t *testing.T) {
	found := headings(t)

	var missing []string
	for _, block := range parser.FlowBlockNames() {
		if mentionedInReference(found, block) {
			continue
		}
		if _, elsewhere := documentedElsewhereInTheDocs[block]; elsewhere {
			continue
		}
		missing = append(missing, block)
	}
	sort.Strings(missing)

	for _, block := range missing {
		t.Errorf("a flow can have a %q block and the reference, which says it documents every "+
			"block type, has no heading for it", block)
	}
}

func TestEveryRootBlockIsInTheReference(t *testing.T) {
	found := headings(t)

	var missing []string
	for _, root := range schema.BuiltinRootSchemas() {
		if mentionedInReference(found, root.Type) {
			continue
		}
		if _, elsewhere := documentedElsewhereInTheDocs[root.Type]; elsewhere {
			continue
		}
		missing = append(missing, root.Type)
	}
	sort.Strings(missing)

	for _, block := range missing {
		t.Errorf("%q is a block a configuration can have and the reference has no heading for it", block)
	}
}

// The reference names an attribute two ways: in a table, between backticks,
// and in an HCL example, as `name = value` or as a block header.
var attributeInReference = regexp.MustCompile("(?m)`([a-z_]+)`|^\\s*([a-z_]+)\\s*=|^\\s*([a-z_]+)\\s*\\{")

func TestTheReferenceDoesNotInventFlowBlocks(t *testing.T) {
	// The other direction: a heading naming something no flow can have.
	found := headings(t)

	known := map[string]bool{}
	for _, block := range parser.FlowBlockNames() {
		known[block] = true
	}
	for _, attr := range parser.FlowAttributeNames() {
		known[attr] = true
	}
	// Nested blocks too: the reference documents `transaction`, which lives
	// inside a destination rather than directly on a flow.
	var walk func(block schema.Block)
	walk = func(block schema.Block) {
		known[block.Type] = true
		for _, attr := range block.Attrs {
			known[attr.Name] = true
		}
		for _, child := range block.Children {
			walk(child)
		}
	}
	for _, root := range schema.BuiltinRootSchemas() {
		walk(root)
	}

	for _, heading := range found {
		if !strings.HasSuffix(heading, " block") {
			continue
		}
		name := strings.TrimSuffix(heading, " block")
		if known[name] {
			continue
		}
		t.Errorf("the reference has a heading for a %q block and no flow can have one", name)
	}
}

func TestEveryAttributeARootBlockDeclaresIsInTheReference(t *testing.T) {
	// "Every block is documented with all supported attributes" — the page's
	// own sentence. An attribute a block accepts and the reference never
	// prints is one nobody finds except by reading the parser.
	content, err := os.ReadFile(referencePage)
	if err != nil {
		t.Fatalf("reading the reference: %v", err)
	}
	page := string(content)

	// Everything the page prints in backticks, which is how it names an
	// attribute.
	printed := map[string]bool{}
	for _, match := range attributeInReference.FindAllStringSubmatch(page, -1) {
		for _, name := range match[1:] {
			if name != "" {
				printed[name] = true
			}
		}
	}
	if len(printed) == 0 {
		t.Fatal("the reference prints no attribute names; this test is checking nothing")
	}

	for _, root := range schema.BuiltinRootSchemas() {
		if _, elsewhere := documentedElsewhereInTheDocs[root.Type]; elsewhere {
			continue
		}

		t.Run(root.Type, func(t *testing.T) {
			var missing []string

			var walk func(block schema.Block)
			walk = func(block schema.Block) {
				for _, attr := range block.Attrs {
					if !printed[attr.Name] {
						missing = append(missing, attr.Name)
					}
				}
				for _, child := range block.Children {
					walk(child)
				}
			}
			walk(root)

			sort.Strings(missing)
			for _, attr := range missing {
				t.Errorf("a %s block accepts %q and the reference never prints it", root.Type, attr)
			}
		})
	}
}

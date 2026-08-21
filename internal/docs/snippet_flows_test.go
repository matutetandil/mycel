package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Every attribute a documented flow block writes has to be one the flow schema
// knows.
//
// The blocks inside a flow are where the mistakes hide: `params = [input.id]`
// on a step, on the PDF page, bound nothing and the query ran with no
// arguments. The test that parses complete configurations does not catch these,
// because the parser sweeps a flow's connector-specific attributes into a bag
// for the connector to read — which is exactly what makes a wrong name silent.
//
// Only the blocks whose attributes are all Mycel's own are checked: `from` and
// `to` legitimately carry whatever the connector at the other end reads.
func TestEveryAttributeADocumentedFlowBlockWritesIsKnown(t *testing.T) {
	flow := schema.FlowSchema()

	// The blocks whose attribute list is closed, and what each accepts.
	checked := map[string]map[string]bool{}
	for _, child := range flow.Children {
		switch child.Type {
		case "from", "to", "step", "enrich", "transform", "response", "batch":
			// from/to/step/enrich carry connector attributes; transform and
			// response are the author's own field names.
			continue
		}
		if child.Open {
			continue
		}
		// This block's own attributes and the names of its children, and no
		// deeper: a child may be open — `dedupe { fingerprint { } }` and
		// `error_handling { error_response { body { } } }` hold field names
		// the author chooses — and what is written inside one is not this
		// block's business.
		accepted := map[string]bool{}
		for _, attr := range child.Attrs {
			accepted[attr.Name] = true
		}
		for _, nested := range child.Children {
			accepted[nested.Type] = true
		}
		checked[child.Type] = accepted
	}
	if len(checked) < 8 {
		t.Fatalf("only %d flow blocks are being checked; the schema has more", len(checked))
	}

	var (
		fence     = regexp.MustCompile("(?s)```hcl\\n(.*?)\\n```")
		attribute = regexp.MustCompile(`(?m)^\s+([a-z_]+)\s*=`)
		nested    = regexp.MustCompile(`(?m)^\s+([a-z_]+)\s*(?:"[^"]*"\s*)?\{`)
	)

	for _, root := range []string{"../../docs", "../../README.md"} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			if strings.Contains(path, "/archive/") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}

			for _, block := range fence.FindAllStringSubmatch(string(body), -1) {
				for name, accepted := range checked {
					for _, decl := range blocksNamed(block[1], name) {
						var unknown []string
						decl = withoutNestedBlocks(decl)
						for _, m := range attribute.FindAllStringSubmatch(decl, -1) {
							if !accepted[m[1]] {
								unknown = append(unknown, m[1])
							}
						}
						for _, m := range nested.FindAllStringSubmatch(decl, -1) {
							if !accepted[m[1]] {
								unknown = append(unknown, m[1])
							}
						}
						sort.Strings(unknown)
						for _, attr := range unique(unknown) {
							t.Errorf("%s: a %s block is written with %q, which is not one of its attributes",
								path, name, attr)
						}
					}
				}
			}
			return nil
		})
	}
}

// withoutNestedBlocks removes everything inside this block's children, so what
// is scanned is the block's own attributes. A name written inside a child
// belongs to the child.
func withoutNestedBlocks(body string) string {
	var out strings.Builder
	depth := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
			continue
		case '}':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			out.WriteByte(body[i])
		}
	}
	return out.String()
}

// blocksNamed returns the bodies of every `<name> {` block in a snippet,
// balanced by brace.
//
// Unlabelled only: `cache "products" { }` at the top level is a named cache
// somebody references, a different block with different attributes from the
// `cache { }` written inside a flow.
func blocksNamed(snippet, name string) []string {
	var out []string
	opener := regexp.MustCompile(`(?m)^\s*` + name + `\s*\{`)
	for _, loc := range opener.FindAllStringIndex(snippet, -1) {
		depth, end := 0, -1
		for i := loc[1] - 1; i < len(snippet); i++ {
			switch snippet[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			continue
		}
		out = append(out, snippet[loc[1]:end])
	}
	return out
}

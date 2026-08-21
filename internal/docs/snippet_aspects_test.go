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

// Every attribute a documented aspect writes has to be one an aspect holds.
//
// The same sweep as the connector and flow ones. This one found nothing, which
// is worth keeping: an aspect is the block most often written from memory —
// it has no connector to anchor it — and `on_drop` had already been missing
// from the schema once while the runtime supported it.
func TestEveryAttributeADocumentedAspectWritesIsKnown(t *testing.T) {
	aspect := schema.AspectSchema()

	accepted := map[string]bool{}
	for _, attr := range aspect.Attrs {
		accepted[attr.Name] = true
	}
	for _, child := range aspect.Children {
		accepted[child.Type] = true
	}

	fence := regexp.MustCompile("(?s)```hcl\\n(.*?)\\n```")
	attribute := regexp.MustCompile(`(?m)^\s+([a-z_]+)\s*=`)
	nested := regexp.MustCompile(`(?m)^\s+([a-z_]+)\s*(?:"[^"]*"\s*)?\{`)

	for _, root := range []string{"../../docs", "../../README.md"} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") || strings.Contains(path, "/archive/") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			for _, block := range fence.FindAllStringSubmatch(string(body), -1) {
				for _, decl := range blocksLabelled(block[1], "aspect") {
					decl = withoutNestedBlocks(decl)
					var unknown []string
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
						t.Errorf("%s: an aspect is written with %q, which is not one of its attributes", path, attr)
					}
				}
			}
			return nil
		})
	}
}

func blocksLabelled(snippet, name string) []string {
	var out []string
	opener := regexp.MustCompile(`(?m)^\s*` + name + `\s+"[^"]*"\s*\{`)
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

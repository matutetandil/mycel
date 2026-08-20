package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Two shapes the documentation keeps drifting into, in snippets.
//
// The test that parses the documentation only sees complete configurations,
// and a fragment — a `from` block on its own, a single type field — is not
// one. So both of these lived in fragments for a long time:
//
//   - `path = "GET /users"`, in the troubleshooting guide, which is the page
//     somebody reads when nothing works. The attribute is `operation`, and a
//     flow written the documented way is refused at startup.
//   - `email = string { required = false }`, the constraint syntax without its
//     parentheses. The parser takes `string({ ... })`. This one was found and
//     cleared out of eighty places once already, and came back in the same
//     section of the same page that gets it right two steps earlier.
func TestSnippetsDoNotUseShapesTheParserRefuses(t *testing.T) {
	var (
		// A method and a path is the value of `operation`, whatever the
		// attribute on the left is called.
		operationValue = regexp.MustCompile(`(?m)^\s*([a-z_]+)\s*=\s*"(GET|POST|PUT|PATCH|DELETE|QUERY|HEAD|OPTIONS) /`)
		// `= string {` rather than `= string({`.
		bareConstraint = regexp.MustCompile(`(?m)=\s*(string|number|bool|boolean|array|object)\s*\{`)
	)

	for _, root := range []string{"../../docs", "../../examples", "../../README.md"} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			// Kept as a record of what was written at the time.
			if strings.Contains(path, "/archive/") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}

			for _, block := range hclFence.FindAllStringSubmatch(string(body), -1) {
				for _, m := range operationValue.FindAllStringSubmatch(block[1], -1) {
					if m[1] == "operation" || m[1] == "target" || m[1] == "query" {
						continue
					}
					t.Errorf("%s: `%s = \"%s /…\"` — a method and a path is the value of `operation`",
						path, m[1], m[2])
				}
				for _, m := range bareConstraint.FindAllStringSubmatch(block[1], -1) {
					t.Errorf("%s: `= %s {` — constraints are written `%s({ … })`",
						path, m[1], m[1])
				}
			}
			return nil
		})
	}
}

var hclFence = regexp.MustCompile("(?s)```hcl\\n(.*?)\\n```")

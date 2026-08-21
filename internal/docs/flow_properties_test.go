package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every property the from and to reference lists has to be read by something.
//
// A `to` block accepts whatever it is given: the parser takes the four
// attributes it knows and sweeps the rest into a bag the connector reads by
// name. So nothing refuses a property that does not exist — it is simply
// ignored, and the flow does something other than what was written.
//
// That is how a destination's `params` came to be documented, mapped in the
// reference to connector.Data.Params, and put there by nothing; and how
// `query_filter` came to be read on a write and not on a read. Both were found
// by running a flow and noticing the answer was wrong.

// readers are the places a from or to property can be read: the accessors on
// the flow configuration, the runtime that builds the connector's request, and
// the connectors themselves.
var readers = []string{
	filepath.Join("..", "flow"),
	filepath.Join("..", "runtime"),
	filepath.Join("..", "connector"),
	filepath.Join("..", "parser"),
}

var propertyRow = regexp.MustCompile("(?m)^\\|\\s*\\**`([a-z_.]+)`\\**\\s*\\|")

// readerSource joins everything that could read a flow property.
func readerSource(t *testing.T) string {
	t.Helper()

	var joined strings.Builder
	for _, root := range readers {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr == nil {
				joined.Write(content)
			}
			return nil
		})
	}
	if joined.Len() == 0 {
		t.Fatal("no source found; this test is checking nothing")
	}
	return joined.String()
}

// propertiesListed returns every property name the page's tables give.
func propertiesListed(t *testing.T, page string) []string {
	t.Helper()

	content, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}

	seen := map[string]bool{}
	for _, row := range propertyRow.FindAllStringSubmatch(string(content), -1) {
		name := strings.TrimSpace(row[1])
		// A dotted path names something inside a block; the leaf is the name.
		if at := strings.LastIndex(name, "."); at >= 0 {
			name = name[at+1:]
		}
		if name != "" {
			seen[name] = true
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func TestEveryFlowPropertyTheReferenceListsIsRead(t *testing.T) {
	source := readerSource(t)

	for _, page := range []string{"source-properties.md", "destination-properties.md"} {
		t.Run(page, func(t *testing.T) {
			listed := propertiesListed(t, filepath.Join("..", "..", "docs", "reference", page))
			if len(listed) == 0 {
				t.Fatal("no properties found on the page")
			}

			var unread []string
			for _, name := range listed {
				if strings.Contains(source, `"`+name+`"`) {
					continue
				}
				unread = append(unread, name)
			}

			for _, name := range unread {
				t.Errorf("the reference lists %q and nothing names it, so a flow written with it "+
					"is accepted and ignored", name)
			}
		})
	}
}

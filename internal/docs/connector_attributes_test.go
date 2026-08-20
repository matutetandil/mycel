package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/connectors"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Every attribute a connector's page lists has to be one the connector accepts.
//
// The page is where somebody copies from, and an attribute that only exists
// there is a configuration the parser refuses or silently ignores — which is
// how the tcp page came to document `codec` for a connector that reads
// `protocol`, the cache page `min_connections` for a pool that takes
// `min_idle`, and every page a `type = "queue"` the parser accepts and the
// factory then refuses.
//
// The schema is what the connector accepts. This compares the two.

// implementedIn maps a page to the directories implementing what it describes.
var implementedIn = map[string][]string{
	"cache":          {"cache"},
	"cdc":            {"cdc"},
	"database":       {"database"},
	"elasticsearch":  {"elasticsearch"},
	"exec":           {"exec"},
	"filesystem":     {"file"},
	"ftp":            {"ftp"},
	"graphql":        {"graphql"},
	"grpc":           {"grpc"},
	"message-queues": {"mq"},
	"mqtt":           {"mqtt"},
	"oauth":          {"oauth"},
	"pdf":            {"pdf"},
	"rest":           {"rest", "http"},
	"s3":             {"s3"},
	"soap":           {"soap"},
	"sse":            {"sse"},
	"tcp":            {"tcp"},
	"websocket":      {"websocket"},
}

var optionHeader = regexp.MustCompile(`(?m)^\|\s*(Option|Attribute|Setting)\s*\|`)

// attributesListed returns the attribute names a page's option tables promise,
// paired with the table each came from so a failure can be found.
func attributesListed(t *testing.T, page string) []string {
	t.Helper()

	content, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	text := string(content)

	seen := map[string]bool{}
	for _, header := range optionHeader.FindAllStringIndex(text, -1) {
		table := text[header[0]:]
		if end := strings.Index(table, "\n\n"); end >= 0 {
			table = table[:end]
		}
		for _, row := range tableRow.FindAllStringSubmatch(table, -1) {
			name := strings.TrimSpace(row[1])
			// A nested attribute is written as its path; the block it belongs
			// to is checked with it.
			if name == "" || strings.ContainsAny(name, "<>${}() ") {
				continue
			}
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

// sourceNaming joins every non-test source file under the named connector
// directories. What reads an attribute is the connector, so that is where a
// documented name has to appear.
func sourceNaming(t *testing.T, dirs []string) string {
	t.Helper()

	var joined strings.Builder
	for _, dir := range dirs {
		root := filepath.Join("..", "connector", dir)
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
	return joined.String()
}

// acceptedBy collects every attribute name a connector's schema declares,
// nested blocks included, both bare and as a dotted path.
func acceptedBy(provider schema.ConnectorSchemaProvider) map[string]bool {
	accepted := map[string]bool{}

	var walk func(prefix string, block schema.Block)
	walk = func(prefix string, block schema.Block) {
		for _, attr := range block.Attrs {
			accepted[attr.Name] = true
			accepted[prefix+attr.Name] = true
		}
		for _, child := range block.Children {
			accepted[child.Type] = true
			accepted[prefix+child.Type] = true
			walk(prefix+child.Type+".", child)
		}
	}
	walk("", provider.ConnectorSchema())

	if source := provider.SourceSchema(); source != nil {
		walk("", *source)
	}
	if target := provider.TargetSchema(); target != nil {
		walk("", *target)
	}
	return accepted
}

// documentedElsewhere lists names a connector's page mentions in an option
// table that belong to something other than the connector block, with the
// reason. An entry here is a decision.
var documentedElsewhere = map[string]string{
	"connector": "names the connector from a flow, not a setting of one",
	"operation": "written on a flow's from or to block",
	"target":    "written on a flow's from or to block",
	"query":     "written on a flow's from or to block",
	"params":    "written on a flow's from or to block",
	"format":    "written on a flow's from or to block",
	"filter":    "written on a flow's from block",
	"when":      "written on a flow",
	"use":       "references a named block",
	"driver":    "the factory chooses the implementation with it, before the connector is built",
	"type":      "the parser dispatches on it",
	"name":      "the connector is registered under it",

	// A flow's filter block, documented on the queue page because that is
	// where a reader meets it. Read by the parser, not by the connector.
	"condition":   "an option of a flow's filter block",
	"on_reject":   "an option of a flow's filter block",
	"id_field":    "an option of a flow's filter block",
	"max_requeue": "an option of a flow's filter block",

	// Written without their prefix in a table whose heading says where they
	// go: nested under schema_registry.schemas.<topic>.
	"key_schema":      "nested under schema_registry.schemas.<topic>",
	"key_schema_id":   "nested under schema_registry.schemas.<topic>",
	"value_schema":    "nested under schema_registry.schemas.<topic>",
	"value_schema_id": "nested under schema_registry.schemas.<topic>",
}

func TestEveryAttributeAConnectorsPageListsIsAccepted(t *testing.T) {
	reg := schema.NewRegistry()
	connectors.RegisterAll(reg)

	pages, err := filepath.Glob(filepath.Join("..", "..", "docs", "connectors", "*.md"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no connector pages found: %v", err)
	}

	// Which connector type each page describes.
	describes := map[string][]string{
		"cache":          {"cache"},
		"cdc":            {"cdc"},
		"database":       {"database"},
		"elasticsearch":  {"elasticsearch"},
		"exec":           {"exec"},
		"filesystem":     {"file"},
		"ftp":            {"ftp"},
		"graphql":        {"graphql"},
		"grpc":           {"grpc"},
		"message-queues": {"mq"},
		"mqtt":           {"mqtt"},
		"oauth":          {"oauth"},
		"pdf":            {"pdf"},
		"rest":           {"rest", "http"},
		"s3":             {"s3"},
		"soap":           {"soap"},
		"sse":            {"sse"},
		"tcp":            {"tcp"},
		"websocket":      {"websocket"},
	}

	for _, page := range pages {
		name := strings.TrimSuffix(filepath.Base(page), ".md")
		types, known := describes[name]
		if _, hasSource := implementedIn[name]; known && !hasSource {
			t.Fatalf("%s has no source directory listed", name)
		}
		if !known {
			continue
		}

		t.Run(name, func(t *testing.T) {
			listed := attributesListed(t, page)
			if len(listed) == 0 {
				t.Skip("lists no attributes")
			}

			// A page may describe several drivers; an attribute belonging to
			// any of them is accepted.
			accepted := map[string]bool{}
			for _, connType := range types {
				for _, registration := range reg.AllRegistrations() {
					if registration.Type != connType {
						continue
					}
					provider := reg.Lookup(registration.Type, registration.Driver)
					if provider == nil {
						continue
					}
					for attr := range acceptedBy(provider) {
						accepted[attr] = true
					}
				}
			}
			if len(accepted) == 0 {
				t.Fatalf("no schema found for %v", types)
			}

			source := sourceNaming(t, implementedIn[name])

			// The parser is the gatekeeper: a name it does not accept is
			// refused however well the connector would have handled it.
			acceptedByParser := map[string]bool{}
			for _, attr := range parser.ConnectorAttributeNames() {
				acceptedByParser[attr] = true
			}

			var unknown, refused []string
			for _, attr := range listed {
				if _, elsewhere := documentedElsewhere[attr]; elsewhere {
					continue
				}
				// The last segment of a dotted path is the attribute itself.
				bare := attr
				if at := strings.LastIndex(attr, "."); at >= 0 {
					bare = attr[at+1:]
				}
				if accepted[attr] || accepted[bare] {
					continue
				}
				// The schema may not declare it and the connector may still
				// read it — that is a gap in the schema, not a page promising
				// something that does not exist. But the parser has to let it
				// through first.
				if strings.Contains(source, `"`+bare+`"`) {
					if strings.Contains(attr, ".") || acceptedByParser[bare] {
						continue
					}
					refused = append(refused, attr)
					continue
				}
				unknown = append(unknown, attr)
			}

			for _, attr := range unknown {
				t.Errorf("the page lists %q and neither the schema nor the connector's source "+
					"names it, so a configuration copied from here does nothing", attr)
			}
			for _, attr := range refused {
				t.Errorf("the page lists %q and the parser does not accept it on a connector, "+
					"so a configuration copied from here does not load at all", attr)
			}
		})
	}
}

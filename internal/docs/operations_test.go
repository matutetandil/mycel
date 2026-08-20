package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every operation a connector's page lists has to be one the connector handles.
//
// A connector's page carries a table of what it can be asked to do, and that
// table is the promise a reader acts on. Twice already it promised something
// nothing implemented: the filesystem page shows a flow writing to a file, and
// the connector could not be a destination at all; the mongodb example had its
// writes commented out with a note blaming the parser. Both were found by
// running them.
//
// This asks the cheaper question — does the name appear in the connector's
// source at all — which cannot prove an operation works, but does catch one
// that exists nowhere.

// connectorSource maps a documentation page to the directories implementing it.
var connectorSource = map[string][]string{
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
	"notifications":  {"slack", "discord", "email", "sms", "push", "webhook"},
	"oauth":          {"oauth"},
	"pdf":            {"pdf"},
	"profile":        {"profile"},
	"rest":           {"rest", "http"},
	"s3":             {"s3"},
	"soap":           {"soap"},
	"sse":            {"sse"},
	"tcp":            {"tcp"},
	"websocket":      {"websocket"},
}

// servedElsewhere lists operations a page names that something other than the
// connector answers, with the reason.
var servedElsewhere = map[string]string{
	// The REST page documents HTTP methods; the runtime dispatches those, and
	// the connector routes whatever it is given.
	"GET":     "an HTTP method, dispatched by the runtime",
	"POST":    "an HTTP method, dispatched by the runtime",
	"PUT":     "an HTTP method, dispatched by the runtime",
	"PATCH":   "an HTTP method, dispatched by the runtime",
	"DELETE":  "an HTTP method, dispatched by the runtime",
	"QUERY":   "an HTTP method, dispatched by the runtime",
	"HEAD":    "an HTTP method, dispatched by the runtime",
	"OPTIONS": "an HTTP method, dispatched by the runtime",

	// Verified against a running service rather than assumed: the connector
	// does the thing whatever the operation is called.
	"PUBLISH": "the queue connector publishes whatever it is given; verified against RabbitMQ",
	"execute": "the exec connector runs its command; verified against a running service",
}

var (
	operationHeader = regexp.MustCompile(`(?m)^\|\s*Operation\s*\|`)
	tableRow        = regexp.MustCompile("(?m)^\\|\\s*`([^`]+)`\\s*\\|")
)

// operationsListed returns the operation names a page's tables promise.
func operationsListed(t *testing.T, page string) []string {
	t.Helper()

	content, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	text := string(content)

	var listed []string
	for _, header := range operationHeader.FindAllStringIndex(text, -1) {
		// The table runs to the first blank line after the header.
		rest := text[header[0]:]
		if end := strings.Index(rest, "\n\n"); end >= 0 {
			rest = rest[:end]
		}
		for _, row := range tableRow.FindAllStringSubmatch(rest, -1) {
			name := strings.TrimSpace(row[1])
			// "INSERT:table", "GET /users" and the like name a shape, not an
			// operation; take the word.
			if at := strings.IndexAny(name, " :"); at > 0 {
				name = name[:at]
			}
			// A placeholder standing in for a name the author supplies.
			if name == "" || name == "*" || strings.ContainsAny(name, "<>") {
				continue
			}
			listed = append(listed, name)
		}
	}
	return listed
}

// sourceOf joins every non-test source file under the named connector
// directories.
func sourceOf(t *testing.T, dirs []string) string {
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

func TestEveryOperationAConnectorsPageListsIsImplemented(t *testing.T) {
	pages, err := filepath.Glob(filepath.Join("..", "..", "docs", "connectors", "*.md"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no connector pages found: %v", err)
	}

	checked := 0
	for _, page := range pages {
		name := strings.TrimSuffix(filepath.Base(page), ".md")
		dirs, known := connectorSource[name]
		if !known {
			continue
		}

		t.Run(name, func(t *testing.T) {
			listed := operationsListed(t, page)
			if len(listed) == 0 {
				t.Skip("lists no operations")
			}
			checked++

			source := sourceOf(t, dirs)
			if source == "" {
				t.Fatalf("no source found under %v", dirs)
			}

			var missing []string
			for _, operation := range listed {
				if _, elsewhere := servedElsewhere[operation]; elsewhere {
					continue
				}
				if strings.Contains(source, `"`+operation+`"`) {
					continue
				}
				// Some connectors compare case-insensitively.
				if strings.Contains(source, `"`+strings.ToUpper(operation)+`"`) ||
					strings.Contains(source, `"`+strings.ToLower(operation)+`"`) {
					continue
				}
				missing = append(missing, operation)
			}

			sort.Strings(missing)
			for _, operation := range missing {
				t.Errorf("the page promises %q and the connector's source never names it, "+
					"so a flow asking for it is answered with an error", operation)
			}
		})
	}
}

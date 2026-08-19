package connectors

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// An attribute a connector declares has to be read by something.
//
// This is the most recurring defect in this codebase: a setting that parses,
// validates, appears in the documentation and is read by nothing. The push
// connector's sender_id and sms_type, MQTT's ca_cert, the breakpoint condition,
// a websocket connection's user — each was written, accepted, and inert, and
// each was found by somebody reading the code rather than by anything
// mechanical.
//
// The schema is the list of what a connector says it takes. This walks it and
// looks for the name in the connector's own source: as a key handed to one of
// the property helpers, or as an index into Properties. A name that appears
// nowhere is a setting nobody could be reading.

// propertyRead matches the ways a connector reaches for a configured value.
var propertyRead = regexp.MustCompile(`"([a-z][a-z0-9_]*)"`)

// sourcesFor returns the connector implementations' source, keyed by directory.
func sourcesFor(t *testing.T) map[string]string {
	t.Helper()

	joined := map[string]*strings.Builder{}
	// The whole of internal, not the connectors alone: a few attributes are
	// read into a typed structure by the parser rather than by the connector —
	// a profile's `select` and `default` among them — and reading only the
	// connector packages would report those as unread.
	root := filepath.Join("..", "..", "internal")

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		dir := filepath.Dir(path)
		if joined[dir] == nil {
			joined[dir] = &strings.Builder{}
		}
		joined[dir].WriteString(string(content))
		return nil
	})
	if err != nil {
		t.Fatalf("reading the connectors: %v", err)
	}

	out := map[string]string{}
	for dir, builder := range joined {
		out[dir] = builder.String()
	}
	if len(out) == 0 {
		t.Fatal("no connector source found; this test is checking nothing")
	}
	return out
}

// mentioned reports whether any of the given directories names this attribute.
func mentioned(sources map[string]string, dirs []string, name string) bool {
	quoted := `"` + name + `"`
	for dir, source := range sources {
		for _, want := range dirs {
			if strings.Contains(dir, want) && strings.Contains(source, quoted) {
				return true
			}
		}
	}
	return false
}

// where a connector type's implementation lives. A type whose directory is not
// named here is checked against every connector's source, which is lenient —
// the point is to catch a name nothing anywhere reads.
var implementations = map[string][]string{
	"database":      {"database"},
	"mq":            {"mq"},
	"rest":          {"rest"},
	"http":          {"http", "rest"},
	"cache":         {"cache"},
	"file":          {"file"},
	"s3":            {"s3"},
	"grpc":          {"grpc"},
	"graphql":       {"graphql"},
	"soap":          {"soap"},
	"tcp":           {"tcp"},
	"websocket":     {"websocket"},
	"sse":           {"sse"},
	"mqtt":          {"mqtt"},
	"cdc":           {"cdc"},
	"ftp":           {"ftp"},
	"elasticsearch": {"elasticsearch"},
	"oauth":         {"oauth"},
	"pdf":           {"pdf"},
	"exec":          {"exec"},
}

// readElsewhere lists attributes a connector declares that something other than
// the connector reads — the runtime, the parser, or the schema itself — with
// the reason. An entry here is a decision, not an oversight.
var readElsewhere = map[string]string{
	"type":   "the parser dispatches on it before any connector sees the configuration",
	"driver": "the factory chooses the implementation with it",
	"name":   "the connector is registered under it",
}

func TestEveryAttributeAConnectorDeclaresIsReadSomewhere(t *testing.T) {
	sources := sourcesFor(t)

	var unread []string
	for name, provider := range everySchema(t) {
		connType := name
		if at := strings.IndexByte(name, '/'); at >= 0 {
			connType = name[:at]
		}

		dirs, known := implementations[connType]
		if !known {
			// Unknown home: look everywhere rather than claim a finding.
			dirs = []string{""}
		}
		// The parser and the runtime read some of what a connector declares.
		dirs = append(dirs, "parser", "runtime")

		var walk func(prefix string, block schema.Block)
		walk = func(prefix string, block schema.Block) {
			for _, attr := range block.Attrs {
				if _, expected := readElsewhere[attr.Name]; expected {
					continue
				}
				if !mentioned(sources, dirs, attr.Name) {
					unread = append(unread, name+": "+prefix+attr.Name)
				}
			}
			for _, child := range block.Children {
				walk(prefix+child.Type+".", child)
			}
		}
		walk("", provider.ConnectorSchema())
	}

	sort.Strings(unread)
	for _, attr := range unread {
		t.Errorf("%s is declared and its name appears nowhere in the connector's source, "+
			"so nothing can be reading it", attr)
	}
}

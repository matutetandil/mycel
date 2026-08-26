package examples

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/connectors"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// Examples are held to the same standard as tests: a feature nobody wrote an
// example for is a feature nobody can see, and — because the harness runs the
// commands in every example's README — a feature nothing exercises end to end.
// So the schema is the list of what exists, and these tests say which parts of
// it no example uses.
//
// A gap is closed by writing the example, not by adding a name to the
// allow-list. An entry there needs a reason that is about the feature, not
// about the effort.

// everyExampleConfig reads every .mycel file under examples/ once.
func everyExampleConfig(t *testing.T) string {
	t.Helper()

	var all strings.Builder
	err := filepath.Walk("../../examples", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".mycel") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		all.Write(body)
		all.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walk examples: %v", err)
	}
	if all.Len() == 0 {
		t.Fatal("no example configuration found")
	}
	return all.String()
}

// connectorsWithoutAnExample: a connector nobody can see working.
var connectorsWithoutAnExample = map[string]string{
	// The default registration of a type is the same schema as its named
	// driver, so it is covered by whichever driver an example writes.
	"database": "the type alone is the postgres schema, covered by database/postgres",
	"mq":       "the type alone is the rabbitmq schema, covered by mq/rabbitmq",
	// Not a connector anyone writes: it is what a `profile` block resolves to.
	"profiled": "an internal result of connector profiles, never written by hand",
}

func TestEveryConnectorHasAnExample(t *testing.T) {
	registry := schema.NewRegistry()
	connectors.RegisterAll(registry)

	config := everyExampleConfig(t)

	var missing []string
	for _, reg := range registry.AllRegistrations() {
		name := reg.Type
		if reg.Driver != "" {
			name = reg.Type + "/" + reg.Driver
		}
		if _, allowed := connectorsWithoutAnExample[name]; allowed {
			continue
		}

		used := regexp.MustCompile(`type\s*=\s*"` + regexp.QuoteMeta(reg.Type) + `"`).MatchString(config)
		if used && reg.Driver != "" {
			used = regexp.MustCompile(`driver\s*=\s*"` + regexp.QuoteMeta(reg.Driver) + `"`).MatchString(config)
		}
		if !used {
			missing = append(missing, name)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no example declares these connectors: %s", strings.Join(missing, ", "))
	}
}

// flowBlocksWithoutAnExample: a block of the flow pipeline nobody demonstrates.
var flowBlocksWithoutAnExample = map[string]string{}

func TestEveryFlowBlockHasAnExample(t *testing.T) {
	config := everyExampleConfig(t)

	var missing []string
	for _, child := range schema.FlowSchema().Children {
		if _, allowed := flowBlocksWithoutAnExample[child.Type]; allowed {
			continue
		}
		// `block {` or `block "label" {`, at the start of a line.
		used := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(child.Type) + `\s*("[^"]*"\s*)?\{`).MatchString(config)
		if !used {
			missing = append(missing, child.Type)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no example uses these flow blocks: %s", strings.Join(missing, ", "))
	}
}

// A block that can be declared at the top level with a name and referred to by
// `use` is a feature in its own right, and the documentation calls that style
// the recommended one. An example that shows five of the twelve shows half of
// it.
//
// The list is derived rather than typed: a block is reusable exactly when its
// schema declares a `use` attribute. Written by hand it was wrong — it had
// `validate` and `enrich`, which are not reusable, and was missing
// `sequence_guard` and `transaction`, which are.
func TestEveryReusableBlockKindHasAnExample(t *testing.T) {
	config := everyExampleConfig(t)

	var missing []string
	for _, kind := range reusableKinds() {
		named := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(kind) + `\s+"[^"]+"\s*\{`)
		if !named.MatchString(config) {
			missing = append(missing, kind)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no example declares these as named, reusable blocks: %s", strings.Join(missing, ", "))
	}
}

// reusableKinds reads the flow schema for every block that accepts `use`.
func reusableKinds() []string {
	seen := map[string]bool{}

	var walk func(schema.Block)
	walk = func(block schema.Block) {
		for _, attr := range block.Attrs {
			if attr.Name == "use" {
				seen[block.Type] = true
			}
		}
		for _, child := range block.Children {
			walk(child)
		}
	}
	walk(schema.FlowSchema())

	kinds := make([]string, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// connectorBlocksWithoutAnExample: a block inside a connector that no example
// writes. Keyed by block name, since the same one appears on several
// connectors — a `tls` block is the same block whether it is on grpc or on
// kafka.
//
// Every entry needs infrastructure the test stack does not run. Where the
// behaviour is covered another way, the reason says where.
var connectorBlocksWithoutAnExample = map[string]string{
	"tls":             "needs an endpoint serving TLS. The client half is covered against a real HTTPS server in internal/connector/http/tls_test.go, including a CA that is used, one that is not trusted, and a configuration that cannot be built",
	"sasl":            "needs a broker with SASL enabled; the Kafka in the test stack is PLAINTEXT",
	"schema_registry": "needs a Schema Registry alongside the broker",
	"cluster":         "needs a Redis cluster; the test stack runs a single node",
	"sentinel":        "needs Redis Sentinel; the test stack runs a single node",
	"ssh":             "needs a host that will run commands over SSH. The stack's SFTP server is restricted to the sftp subsystem, which is the point of it",
}

// A connector's blocks are features too: `consumer`, `producer`, `headers`,
// `federation`, `env`. An example that declares the connector and none of them
// shows the connector's front door and nothing behind it.
func TestEveryConnectorBlockHasAnExample(t *testing.T) {
	registry := schema.NewRegistry()
	connectors.RegisterAll(registry)

	config := everyExampleConfig(t)

	missing := map[string][]string{}
	for _, reg := range registry.AllRegistrations() {
		provider := registry.Lookup(reg.Type, reg.Driver)
		if provider == nil {
			continue
		}
		name := reg.Type
		if reg.Driver != "" {
			name = reg.Type + "/" + reg.Driver
		}
		for _, child := range provider.ConnectorSchema().Children {
			if _, allowed := connectorBlocksWithoutAnExample[child.Type]; allowed {
				continue
			}
			// `profile "magento" {` carries a label; `headers {` does not.
			used := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(child.Type) + `\s*("[^"]*"\s*)?\{`).MatchString(config)
			if !used {
				missing[child.Type] = append(missing[child.Type], name)
			}
		}
	}

	if len(missing) == 0 {
		return
	}
	var report []string
	for block, on := range missing {
		sort.Strings(on)
		report = append(report, block+" (on "+strings.Join(on, ", ")+")")
	}
	sort.Strings(report)
	t.Errorf("no example writes these connector blocks: %s", strings.Join(report, "; "))
}

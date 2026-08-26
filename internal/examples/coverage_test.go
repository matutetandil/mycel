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

// Ten block types can be declared at the top level and referred to by name,
// and the documentation calls that the recommended style. An example that
// shows five of them shows half a feature.
func TestEveryReusableBlockKindHasAnExample(t *testing.T) {
	config := everyExampleConfig(t)

	kinds := []string{
		"accept", "coordinate", "dedupe", "enrich", "error_handling",
		"lock", "response", "retry", "semaphore", "validate",
	}

	var missing []string
	for _, kind := range kinds {
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

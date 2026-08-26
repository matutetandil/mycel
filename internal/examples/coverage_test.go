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

// celFunctionsWithoutAnExample: a function the language offers that no example
// calls.
var celFunctionsWithoutAnExample = map[string]string{
	// `env` reads an environment variable, and every example that needs one
	// writes it in HCL — `env("PORT", 3000)` — where it is HCL's function of
	// the same name, folded in when the file is read. The CEL one is for the
	// places where the value is not known until the expression runs, which is
	// a connector profile's `select`.
	"env": "written as HCL's env() in every example; the CEL one exists for expressions evaluated per message",
}

// Every function the transform language offers has to be shown working
// somewhere. Fifteen of the thirty-five were in no example at all — split,
// default, as_list, join, format_date, the ones a first transform reaches for
// — and the only place to see one was the reference table.
func TestEveryCELFunctionHasAnExample(t *testing.T) {
	declared := regexp.MustCompile(`cel\.Function\("([a-z_0-9]+)"`)
	source, err := os.ReadFile("../transform/cel.go")
	if err != nil {
		t.Fatalf("read the CEL environment: %v", err)
	}

	config := everyExampleConfig(t)

	var missing []string
	for _, match := range declared.FindAllStringSubmatch(string(source), -1) {
		name := match[1]
		if _, allowed := celFunctionsWithoutAnExample[name]; allowed {
			continue
		}
		called := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
		if !called.MatchString(config) {
			missing = append(missing, name)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no example calls these CEL functions: %s", strings.Join(missing, ", "))
	}
}

// authBlocksWithoutAnExample: a block inside auth that no example writes.
var authBlocksWithoutAnExample = map[string]string{
	"sso":   "needs a real identity provider — an OIDC issuer or a SAML metadata URL — which the test stack does not run",
	"apple": "needs an Apple developer team, a services ID and a signing key; the other two social providers are shown",
	// The seventeen endpoint blocks take the same three attributes and differ
	// only in which route they move. The example moves one, which is the shape;
	// the other sixteen would be repetition.
	"logout":          "one endpoint block is shown; the rest take the same three attributes",
	"register":        "one endpoint block is shown; the rest take the same three attributes",
	"refresh":         "one endpoint block is shown; the rest take the same three attributes",
	"me":              "one endpoint block is shown; the rest take the same three attributes",
	"password_forgot": "one endpoint block is shown; the rest take the same three attributes",
	"password_reset":  "one endpoint block is shown; the rest take the same three attributes",
	"password_change": "one endpoint block is shown; the rest take the same three attributes",
	"sessions_list":   "one endpoint block is shown; the rest take the same three attributes",
	"sessions_revoke": "one endpoint block is shown; the rest take the same three attributes",
	"mfa_setup":       "one endpoint block is shown; the rest take the same three attributes",
	"mfa_verify":      "one endpoint block is shown; the rest take the same three attributes",
	"mfa_disable":     "one endpoint block is shown; the rest take the same three attributes",
	"mfa_recovery":    "one endpoint block is shown; the rest take the same three attributes",
	"social_callback": "one endpoint block is shown; the rest take the same three attributes",
	"sso_callback":    "one endpoint block is shown; the rest take the same three attributes",
	// Per-endpoint rate limits, likewise: three attributes, six copies.
	"login": "shown as an endpoint override; the per-endpoint rate limits take the same shape",
	// The eight hooks take the same two attributes and differ in when they
	// run. The example shows the three kinds that differ in more than that: an
	// after_ hook, a before_ one that can refuse, and the reset hook, which is
	// handed a token it has to deliver.
	"after_login":            "three of the eight hooks are shown, covering the kinds that differ in more than their name",
	"before_password_change": "three of the eight hooks are shown, covering the kinds that differ in more than their name",
	"after_password_change":  "three of the eight hooks are shown, covering the kinds that differ in more than their name",
	"on_suspicious_activity": "three of the eight hooks are shown, covering the kinds that differ in more than their name",
}

// auth is the largest block in the language and the one where a setting that
// does nothing costs the most. Its parts have to be shown working, not just
// described.
func TestEveryAuthBlockHasAnExample(t *testing.T) {
	config := everyExampleConfig(t)

	var missing []string
	var walk func(path string, block schema.Block)
	walk = func(path string, block schema.Block) {
		for _, child := range block.Children {
			if _, allowed := authBlocksWithoutAnExample[child.Type]; allowed {
				continue
			}
			used := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(child.Type) + `\s*("[^"]*"\s*)?\{`).MatchString(config)
			if !used {
				missing = append(missing, path+"."+child.Type)
				continue // its own children cannot be written either
			}
			walk(path+"."+child.Type, child)
		}
	}
	walk("auth", schema.AuthSchema())

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no example writes these auth blocks: %s", strings.Join(missing, ", "))
	}
}

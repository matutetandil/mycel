package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/pkg/connectors"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// One list of checks, run by `mycel validate` and by the runtime.
//
// A configuration that passes validate and then refuses to start is worse than
// either outcome on its own: it is the command whose whole job is to tell you
// beforehand, telling you wrong. The two ran their own lists, in their own
// order, and nothing kept them together.

func TestValidateAndStartRunTheSameChecks(t *testing.T) {
	// Not a comparison of two lists — there is only one now, and this is what
	// keeps it that way: every exported Validate* function in this package has
	// to appear in it, so adding one and forgetting to wire it up fails here.
	reg := schema.NewRegistryWith(connectors.RegisterAll)
	kinds := map[string]bool{}
	for _, check := range Checks(nil, reg) {
		if check.Kind == "" {
			t.Error("a check has no kind, so its errors cannot be counted")
		}
		if kinds[check.Kind] {
			t.Errorf("two checks are both called %q", check.Kind)
		}
		kinds[check.Kind] = true
	}

	// The count is written down so that adding a check without a thought about
	// this test is a failure rather than a silent pass.
	if len(kinds) != 12 {
		t.Errorf("%d checks, and this test knows about 12 — add the new one to "+
			"the list in validate_all.go and update this number", len(kinds))
	}
}

func TestEverythingWrongComesBackAtOnce(t *testing.T) {
	reg := schema.NewRegistryWith(connectors.RegisterAll)
	config := &parser.Configuration{
		Connectors: []*connector.Config{
			{Name: "api", Type: "rest", Properties: map[string]interface{}{"port": 8080}},
		},
		Flows: []*flow.Config{{
			Name:  "get_user",
			From:  &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "GET /x"}},
			Cache: &flow.CacheConfig{Storage: "nobody", TTL: "5 minutes"},
			Steps: []*flow.StepConfig{
				{Name: "one", Connector: "api", OnError: "ignore"},
				{Name: "one", Connector: "api"},
			},
			Validate: &flow.ValidateConfig{Input: "no_such_type"},
		}},
	}

	errs := ValidateAll(config, reg)
	if len(errs) < 5 {
		t.Fatalf("%d errors, want one for each of the five mistakes: %v", len(errs), errs)
	}

	joined := strings.Join(errorStrings(errs), "\n")
	for _, want := range []string{"5 minutes", "ignore", "no_such_type", "declared 2 times", "nobody"} {
		if !strings.Contains(joined, want) {
			t.Errorf("nothing reported %q:\n%s", want, joined)
		}
	}
}

func TestAGoodConfigurationPassesEveryCheck(t *testing.T) {
	reg := schema.NewRegistryWith(connectors.RegisterAll)
	config := &parser.Configuration{
		Connectors: []*connector.Config{
			{Name: "api", Type: "rest", Properties: map[string]interface{}{"port": 8080}},
			{Name: "db", Type: "database", Driver: "sqlite", Properties: map[string]interface{}{"database": ":memory:"}},
		},
		Flows: []*flow.Config{{
			Name: "list_orders",
			From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "GET /orders"}},
			To:   &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		}},
	}

	if errs := ValidateAll(config, reg); len(errs) != 0 {
		t.Errorf("a working configuration was refused: %v", errs)
	}
}

func errorStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

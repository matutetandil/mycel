package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// Named operations are the feature where a connector declares what it can do
// once and flows refer to it by name. The resolver that turns a name into the
// executable form was written and only ever called by the startup banner, so
// the name reached the connector verbatim: on a REST source that meant
// registering a route literally called "get_user", which panics the HTTP mux
// before the service finishes starting.

func resolverRuntime(t *testing.T, connectors []*connector.Config, flows []*flow.Config) *Runtime {
	t.Helper()

	res := connector.NewOperationResolver()
	for _, c := range connectors {
		res.Register(c)
	}
	return &Runtime{
		config:            &parser.Configuration{Connectors: connectors, Flows: flows},
		operationResolver: res,
	}
}

func restConnector() *connector.Config {
	return &connector.Config{
		Name: "api", Type: "rest",
		Operations: []*connector.OperationDef{
			{Name: "get_user", Method: "GET", Path: "/users/:id"},
			{Name: "list_users", Method: "get", Path: "/users"},
		},
	}
}

func dbConnector() *connector.Config {
	return &connector.Config{
		Name: "db", Type: "database", Driver: "sqlite",
		Operations: []*connector.OperationDef{
			{Name: "user_by_id", Query: "SELECT * FROM users WHERE id = :id"},
			{Name: "insert_user", Table: "users"},
		},
	}
}

func TestNamedOperationOnASourceBecomesTheInlineForm(t *testing.T) {
	f := &flow.Config{
		Name: "get_user",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "get_user"}},
	}
	r := resolverRuntime(t, []*connector.Config{restConnector()}, []*flow.Config{f})

	if err := r.resolveNamedOperations(); err != nil {
		t.Fatalf("resolveNamedOperations: %v", err)
	}
	if got := f.From.GetOperation(); got != "GET /users/:id" {
		t.Errorf("operation = %q, want the inline form", got)
	}
}

func TestAnInlineOperationIsLeftAlone(t *testing.T) {
	// The rewrite must be invisible to everyone not using named operations: a
	// value resolves only when the connector declares that exact name.
	f := &flow.Config{
		Name: "inline",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "POST /orders"}},
	}
	r := resolverRuntime(t, []*connector.Config{restConnector()}, []*flow.Config{f})

	if err := r.resolveNamedOperations(); err != nil {
		t.Fatalf("resolveNamedOperations: %v", err)
	}
	if got := f.From.GetOperation(); got != "POST /orders" {
		t.Errorf("operation = %q, want it untouched", got)
	}
}

func TestADatabaseOperationLandsInTheRightParameter(t *testing.T) {
	// A database operation declares either a table or a raw query, and the
	// runtime reads those from two different parameters. A query left under
	// `target` is used as a table name, which fails as a SQL syntax error
	// quoting the query back.
	byID := &flow.Config{
		Name: "read",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "get_user"}},
		To:   &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "user_by_id"}},
	}
	insert := &flow.Config{
		Name: "write",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "list_users"}},
		To:   &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "insert_user"}},
	}
	r := resolverRuntime(t, []*connector.Config{restConnector(), dbConnector()}, []*flow.Config{byID, insert})

	if err := r.resolveNamedOperations(); err != nil {
		t.Fatalf("resolveNamedOperations: %v", err)
	}

	if got := byID.To.GetQuery(); got != "SELECT * FROM users WHERE id = :id" {
		t.Errorf("query = %q, want the raw SQL", got)
	}
	// And the name must not stay behind pretending to be a table.
	if got := byID.To.GetTarget(); got != "" {
		t.Errorf("target = %q, want it cleared once the value moved to query", got)
	}

	if got := insert.To.GetTarget(); got != "users" {
		t.Errorf("target = %q, want the table", got)
	}
	if got := insert.To.GetQuery(); got != "" {
		t.Errorf("query = %q, want a table operation to leave it empty", got)
	}
}

func TestTheMethodIsNormalised(t *testing.T) {
	f := &flow.Config{
		Name: "list",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "list_users"}},
	}
	r := resolverRuntime(t, []*connector.Config{restConnector()}, []*flow.Config{f})

	if err := r.resolveNamedOperations(); err != nil {
		t.Fatalf("resolveNamedOperations: %v", err)
	}
	if got := f.From.GetOperation(); got != "GET /users" {
		t.Errorf("operation = %q, want an upper-cased method", got)
	}
}

func TestStepsAndExtraDestinationsResolveToo(t *testing.T) {
	f := &flow.Config{
		Name:  "fan",
		From:  &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "list_users"}},
		Steps: []*flow.StepConfig{{Connector: "db", ConnectorParams: map[string]interface{}{"target": "user_by_id"}}},
		MultiTo: []*flow.ToConfig{
			{Connector: "db", ConnectorParams: map[string]interface{}{"target": "insert_user"}},
		},
	}
	r := resolverRuntime(t, []*connector.Config{restConnector(), dbConnector()}, []*flow.Config{f})

	if err := r.resolveNamedOperations(); err != nil {
		t.Fatalf("resolveNamedOperations: %v", err)
	}
	if got := f.Steps[0].GetQuery(); got != "SELECT * FROM users WHERE id = :id" {
		t.Errorf("step query = %q", got)
	}
	if got := f.MultiTo[0].GetTarget(); got != "users" {
		t.Errorf("multi-to target = %q", got)
	}
}

func TestAnUnresolvableOperationIsReportedByName(t *testing.T) {
	// A REST operation with no path cannot be formatted. Failing here names the
	// flow, the operation and the connector; failing later is a mux panic.
	broken := &connector.Config{
		Name: "api", Type: "rest",
		Operations: []*connector.OperationDef{{Name: "half_written", Method: "GET"}},
	}
	f := &flow.Config{
		Name: "f",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "half_written"}},
	}
	r := resolverRuntime(t, []*connector.Config{broken}, []*flow.Config{f})

	err := r.resolveNamedOperations()
	if err == nil {
		t.Fatal("an operation that cannot be formatted was accepted")
	}
	for _, want := range []string{`"f"`, `"half_written"`, `"api"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestTheOperationDefinitionStaysReachable(t *testing.T) {
	// The parameters an operation declares have to outlive the name, or there
	// is nothing left to validate a request against.
	f := &flow.Config{
		Name: "f",
		From: &flow.FromConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "get_user"}},
	}
	r := resolverRuntime(t, []*connector.Config{restConnector()}, []*flow.Config{f})
	if err := r.resolveNamedOperations(); err != nil {
		t.Fatalf("resolveNamedOperations: %v", err)
	}

	def := OperationDefFor(f.From.ConnectorParams)
	if def == nil {
		t.Fatal("the operation definition was lost")
	}
	if def.Name != "get_user" {
		t.Errorf("definition = %q", def.Name)
	}
}

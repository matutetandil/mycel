package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/saga"
	"github.com/matutetandil/mycel/v3/internal/statemachine"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A step or a destination can set request headers per request.
//
// The value that selects a tenant, locale or store view comes from the
// request — `input.store` — so it cannot be fixed on the connector. A
// `headers` attribute written on the step was swept into the connector
// params, which nothing read, and `mycel validate` said nothing. The
// workaround was one connector per possible value and one step per
// connector, each guarded with a `when`.

// headerSeeingConnector records the headers each request carried.
type headerSeeingConnector struct {
	mu   sync.Mutex
	seen []map[string]string
}

func (c *headerSeeingConnector) Name() string                  { return "backend" }
func (c *headerSeeingConnector) Type() string                  { return "http" }
func (c *headerSeeingConnector) Connect(context.Context) error { return nil }
func (c *headerSeeingConnector) Close(context.Context) error   { return nil }
func (c *headerSeeingConnector) Health(context.Context) error  { return nil }

func (c *headerSeeingConnector) note(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, connector.RequestHeaders(ctx))
}

func (c *headerSeeingConnector) Call(ctx context.Context, _ string, _ map[string]interface{}) (interface{}, error) {
	c.note(ctx)
	return map[string]interface{}{"title": "t"}, nil
}

func (c *headerSeeingConnector) Write(ctx context.Context, _ *connector.Data) (*connector.Result, error) {
	c.note(ctx)
	return &connector.Result{Affected: 1}, nil
}

func (c *headerSeeingConnector) headers() []map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]string(nil), c.seen...)
}

func TestAStepSendsTheHeadersItDeclares(t *testing.T) {
	backend := &headerSeeingConnector{}
	handler := writingHandler(t, []*flow.StepConfig{
		{
			Name: "page", Connector: "db",
			ConnectorParams: map[string]interface{}{
				"operation": "POST /graphql",
				"headers": map[string]interface{}{
					"Store":     "input.store",
					"X-Version": 2,
					"X-Absent":  "input.missing",
					"X-Fixed":   "yes",
				},
				"body": map[string]interface{}{"query": "'{ page { title } }'"},
			},
		},
		{
			// A step with no headers of its own sends none, whatever the step
			// before it declared.
			Name: "plain", Connector: "db",
			ConnectorParams: map[string]interface{}{"operation": "GET /health"},
		},
	}, backend)

	if _, err := handler.executeSteps(context.Background(), map[string]interface{}{"store": "au", "missing": nil}); err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	seen := backend.headers()
	if len(seen) != 2 {
		t.Fatalf("the connector was called %d times", len(seen))
	}
	first := seen[0]
	if first["Store"] != "au" {
		t.Errorf("Store = %q, want the value the request carried", first["Store"])
	}
	if first["X-Version"] != "2" {
		t.Errorf("a number became %q", first["X-Version"])
	}
	if first["X-Fixed"] != "yes" {
		t.Errorf("a constant became %q", first["X-Fixed"])
	}
	// A header whose value is null is not sent at all, rather than sent as
	// the word "null" or an empty value.
	if _, sent := first["X-Absent"]; sent {
		t.Errorf("a null header was sent as %q", first["X-Absent"])
	}
	if len(seen[1]) != 0 {
		t.Errorf("the second step inherited headers it did not declare: %v", seen[1])
	}
}

func TestAStepHeaderThatCannotBeEvaluatedNamesItself(t *testing.T) {
	backend := &headerSeeingConnector{}
	handler := writingHandler(t, []*flow.StepConfig{{
		Name: "page", Connector: "db",
		ConnectorParams: map[string]interface{}{
			"operation": "GET /page",
			"headers":   map[string]interface{}{"Store": "input.store.nope("},
		},
	}}, backend)

	_, err := handler.executeSteps(context.Background(), map[string]interface{}{"store": "au"})
	if err == nil {
		t.Fatal("a header expression that does not parse was accepted")
	}
	for _, want := range []string{"page", "header", "Store"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestADestinationSendsTheHeadersItDeclares(t *testing.T) {
	backend := &headerSeeingConnector{}
	dest := &flow.ToConfig{
		Connector: "sink",
		ConnectorParams: map[string]interface{}{
			"target":    "/products",
			"operation": "POST",
			"headers": map[string]interface{}{
				"Store":   "input.store",
				"X-Fixed": "yes",
			},
		},
	}

	h := destinationHandler(t, dest, backend, true)
	_, err := h.writeToDestination(context.Background(),
		map[string]interface{}{"store": "au", "sku": "A1"},
		map[string]interface{}{"sku": "A1"},
		dest, Operation{Method: "POST"})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	seen := backend.headers()
	if len(seen) != 1 {
		t.Fatalf("the connector was written to %d times", len(seen))
	}
	if seen[0]["Store"] != "au" || seen[0]["X-Fixed"] != "yes" {
		t.Errorf("headers = %v", seen[0])
	}
}

// `headers` is honoured by the connectors that speak HTTP. On any other it
// would be swept up and ignored, which is what this whole feature replaces,
// so validate refuses it there rather than letting the next one be a silent
// no-op.
func TestValidateRefusesHeadersOnAConnectorThatDoesNotSendAny(t *testing.T) {
	reg := NewSchemaRegistry()
	headers := map[string]interface{}{"Store": "input.store"}

	cfg := &parser.Configuration{
		Connectors: []*connector.Config{
			{Name: "db", Type: "database", Driver: "postgres"},
			{Name: "api", Type: "http"},
			{Name: "gql", Type: "graphql", Driver: "client"},
		},
		Flows: []*flow.Config{
			{
				Name: "fine",
				Steps: []*flow.StepConfig{
					{Name: "a", Connector: "api", ConnectorParams: map[string]interface{}{"operation": "GET /a", "headers": headers}},
					{Name: "b", Connector: "gql", ConnectorParams: map[string]interface{}{"operation": "query { a }", "headers": headers}},
				},
				To: &flow.ToConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "POST /a", "headers": headers}},
			},
			{
				Name: "wrong_step",
				Steps: []*flow.StepConfig{
					{Name: "rows", Connector: "db", ConnectorParams: map[string]interface{}{"query": "SELECT 1", "headers": headers}},
				},
				To: &flow.ToConfig{Connector: "api", ConnectorParams: map[string]interface{}{"operation": "POST /a"}},
			},
			{
				Name: "wrong_to",
				To:   &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "t", "headers": headers}},
			},
		},
	}

	errs := ValidateFlowSchemas(cfg, reg)
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want one per misplaced headers", errs)
	}
	joined := errs[0].Error() + "\n" + errs[1].Error()
	for _, want := range []string{"wrong_step", `step "rows"`, "wrong_to", "headers", "database/postgres", "http"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors do not mention %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "fine") {
		t.Errorf("the flow whose headers go to HTTP connectors was refused:\n%s", joined)
	}
}

// An enrichment is the same call a step makes, so it declares headers the
// same way.
func TestAnEnrichmentSendsTheHeadersItDeclares(t *testing.T) {
	backend := &headerSeeingConnector{}
	h := enrichingHandler(t, nil, map[string]connector.Connector{"backend": backend})

	_, err := h.executeEnrichments(context.Background(),
		map[string]interface{}{"store": "au", "nope": nil},
		[]*flow.EnrichConfig{{
			Name: "page", Connector: "backend",
			ConnectorParams: map[string]interface{}{
				"operation": "GET /page",
				"headers":   map[string]interface{}{"Store": "input.store", "X-Fixed": "yes", "X-Absent": "input.nope"},
			},
		}})
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}
	seen := backend.headers()
	if len(seen) != 1 || seen[0]["Store"] != "au" || seen[0]["X-Fixed"] != "yes" {
		t.Errorf("headers = %v", seen)
	}
	if _, sent := seen[0]["X-Absent"]; sent {
		t.Errorf("a null header was sent as %q", seen[0]["X-Absent"])
	}
}

func TestValidateRefusesHeadersOnAnEnrichmentToADatabase(t *testing.T) {
	reg := NewSchemaRegistry()
	headers := map[string]interface{}{"Store": "input.store"}
	cfg := &parser.Configuration{
		Connectors: []*connector.Config{
			{Name: "db", Type: "database", Driver: "sqlite"},
			{Name: "soap", Type: "soap"},
		},
		Flows: []*flow.Config{{
			Name: "lookup",
			Enrichments: []*flow.EnrichConfig{
				{Name: "fine", Connector: "soap", ConnectorParams: map[string]interface{}{"operation": "GetOrder", "headers": headers}},
				{Name: "wrong", Connector: "db", ConnectorParams: map[string]interface{}{"operation": "orders", "headers": headers}},
			},
			To: &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "t"}},
		}},
		Transforms: []*transform.Config{{
			Name: "shared",
			Enrichments: []*transform.EnrichConfig{
				{Name: "named_wrong", Connector: "db", Operation: "orders", Headers: map[string]string{"Store": "input.store"}},
			},
		}},
	}

	errs := ValidateFlowSchemas(cfg, reg)
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want one per misplaced headers", errs)
	}
	joined := errs[0].Error() + "\n" + errs[1].Error()
	for _, want := range []string{`enrich "wrong"`, `enrich "named_wrong"`, `transform "shared"`, "database/sqlite"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors do not mention %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"fine"`) {
		t.Errorf("the enrichment whose headers go to SOAP was refused:\n%s", joined)
	}
}

func TestValidateRefusesHeadersOnASagaOrTransitionActionToADatabase(t *testing.T) {
	reg := NewSchemaRegistry()
	headers := map[string]interface{}{"Store": "input.store"}
	cfg := &parser.Configuration{
		Connectors: []*connector.Config{
			{Name: "db", Type: "database", Driver: "sqlite"},
			{Name: "api", Type: "http"},
		},
		Sagas: []*saga.Config{{
			Name: "order",
			Steps: []*saga.StepConfig{{
				Name:       "charge",
				Action:     &saga.ActionConfig{Connector: "api", Operation: "POST /charges", Headers: headers},
				Compensate: &saga.ActionConfig{Connector: "db", Operation: "DELETE", Target: "charges", Headers: headers},
			}},
			OnComplete: &saga.ActionConfig{Connector: "db", Operation: "INSERT", Target: "audit", Headers: headers},
		}},
		StateMachines: []*statemachine.Config{{
			Name: "order_status",
			States: map[string]*statemachine.StateConfig{
				"new": {Name: "new", Transitions: map[string]*statemachine.TransitionConfig{
					"ship": {TransitionTo: "shipped", Action: &statemachine.ActionConfig{Connector: "db", Operation: "UPDATE", Target: "orders", Headers: headers}},
					"note": {TransitionTo: "noted", Action: &statemachine.ActionConfig{Connector: "api", Operation: "POST /notes", Headers: headers}},
				}},
			},
		}},
	}

	errs := ValidateFlowSchemas(cfg, reg)
	if len(errs) != 3 {
		t.Fatalf("errors = %v, want one per misplaced headers (compensate, on_complete, ship)", errs)
	}
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{`saga "order"`, `compensate of step "charge"`, "on_complete", `state_machine "order_status"`, `transition "ship"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors do not mention %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `step "charge" `) && strings.Contains(joined, `action of step "charge"`) {
		t.Errorf("the action whose headers go to http was refused:\n%s", joined)
	}
}

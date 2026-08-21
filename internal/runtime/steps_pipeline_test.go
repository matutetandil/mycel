package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A step block is how a flow gathers what it needs before producing an answer:
// look the customer up here, ask that service for their balance, then shape a
// reply out of both. Each step is named and the ones after it refer to it, so
// the order, the skipping and the failure handling are the whole feature — and
// a step that quietly returns nothing produces a response with a missing field
// rather than an error anyone can act on.

// stepConnector answers calls and reads with whatever it is given, and records
// what it was asked.
type stepConnector struct {
	name string
	err  error
	rows []map[string]interface{}
	call interface{}

	mu     sync.Mutex
	calls  int
	params []map[string]interface{}
	ops    []string
}

func (s *stepConnector) Name() string                  { return s.name }
func (s *stepConnector) Type() string                  { return "fake" }
func (s *stepConnector) Connect(context.Context) error { return nil }
func (s *stepConnector) Close(context.Context) error   { return nil }
func (s *stepConnector) Health(context.Context) error  { return nil }

func (s *stepConnector) record(op string, params map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ops = append(s.ops, op)
	s.params = append(s.params, params)
}

func (s *stepConnector) seen() (int, []map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.params
}

func (s *stepConnector) Read(_ context.Context, q connector.Query) (*connector.Result, error) {
	s.record(q.Operation, q.Filters)
	if s.err != nil {
		return nil, s.err
	}
	return &connector.Result{Rows: s.rows}, nil
}

func (s *stepConnector) Call(_ context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	s.record(operation, params)
	if s.err != nil {
		return nil, s.err
	}
	return s.call, nil
}

func stepHandler(t *testing.T, steps []*flow.StepConfig, conns map[string]connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	for name, conn := range conns {
		registry.Replace(name, conn)
	}
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return &FlowHandler{
		Config:      &flow.Config{Name: "profile", Steps: steps},
		Connectors:  registry,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestAFlowWithNoStepsGathersNothing(t *testing.T) {
	h := stepHandler(t, nil, nil)
	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
}

func TestEachStepIsNamedAndTheNextOneCanUseIt(t *testing.T) {
	// This is what makes a chain rather than a list: the second call is built
	// out of the first one's answer.
	customers := &stepConnector{name: "db", rows: []map[string]interface{}{{"id": "c-1", "tier": "gold"}}}
	billing := &stepConnector{name: "billing", call: map[string]interface{}{"balance": 42}}

	h := stepHandler(t, []*flow.StepConfig{
		{
			Name: "customer", Connector: "db",
			ConnectorParams: map[string]interface{}{
				"query":  "SELECT * FROM customers WHERE email = :email",
				"params": map[string]interface{}{"email": "input.email"},
			},
		},
		{
			Name: "balance", Connector: "billing",
			ConnectorParams: map[string]interface{}{
				"operation": "GET /balance",
				"params":    map[string]interface{}{"customer_id": "step.customer.id"},
			},
		},
	}, map[string]connector.Connector{"db": customers, "billing": billing})

	results, err := h.executeSteps(context.Background(), map[string]interface{}{"email": "someone@example.com"})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	// A single row comes back as the row itself, so later expressions read
	// step.customer.tier rather than step.customer[0].tier.
	customer, ok := results["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("customer = %#v, want the single row itself", results["customer"])
	}
	if customer["tier"] != "gold" {
		t.Errorf("customer = %v", customer)
	}

	// And the second step was given what the first one found.
	_, params := billing.seen()
	if len(params) != 1 || params[0]["customer_id"] != "c-1" {
		t.Errorf("the second step was called with %v, want the id from the first", params)
	}
}

func TestSeveralRowsComeBackAsAList(t *testing.T) {
	db := &stepConnector{name: "db", rows: []map[string]interface{}{{"id": "1"}, {"id": "2"}}}
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "orders", Connector: "db",
		ConnectorParams: map[string]interface{}{"query": "SELECT * FROM orders"},
	}}, map[string]connector.Connector{"db": db})

	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	rows, ok := results["orders"].([]map[string]interface{})
	if !ok || len(rows) != 2 {
		t.Errorf("orders = %#v, want both rows", results["orders"])
	}
}

func TestAStepWhoseConditionIsFalseIsNotCalled(t *testing.T) {
	// The point of the condition is to not pay for the call, so it has to be
	// checked before the connector is reached.
	prices := &stepConnector{name: "prices", call: map[string]interface{}{"price": 10}}
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "prices", Connector: "prices", When: "input.include_prices == true",
		ConnectorParams: map[string]interface{}{"operation": "GET /prices"},
	}}, map[string]connector.Connector{"prices": prices})

	results, err := h.executeSteps(context.Background(), map[string]interface{}{"include_prices": false})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	if calls, _ := prices.seen(); calls != 0 {
		t.Errorf("the step was called %d times although its condition was false", calls)
	}

	// The name still has to be bound, or every later expression that mentions
	// it fails with "no such key" instead of seeing nothing.
	value, present := results["prices"]
	if !present {
		t.Error("the skipped step left its name unbound, so later expressions cannot test for it")
	}
	if value != nil {
		t.Errorf("prices = %v, want nothing", value)
	}
}

func TestASkippedStepCanStandInWithADefault(t *testing.T) {
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "prices", Connector: "prices", When: "false",
		Default:         map[string]interface{}{"price": 0},
		ConnectorParams: map[string]interface{}{"operation": "GET /prices"},
	}}, map[string]connector.Connector{"prices": &stepConnector{name: "prices"}})

	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	value, ok := results["prices"].(map[string]interface{})
	if !ok || value["price"] != 0 {
		t.Errorf("prices = %#v, want the declared default", results["prices"])
	}
}

func TestAConditionThatDependsOnAnEarlierStep(t *testing.T) {
	customers := &stepConnector{name: "db", rows: []map[string]interface{}{{"id": "c-1", "tier": "gold"}}}
	perks := &stepConnector{name: "perks", call: map[string]interface{}{"perks": []string{"lounge"}}}

	h := stepHandler(t, []*flow.StepConfig{
		{Name: "customer", Connector: "db", ConnectorParams: map[string]interface{}{"query": "SELECT 1"}},
		{
			Name: "perks", Connector: "perks", When: `step.customer.tier == "gold"`,
			ConnectorParams: map[string]interface{}{"operation": "GET /perks"},
		},
	}, map[string]connector.Connector{"db": customers, "perks": perks})

	if _, err := h.executeSteps(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	if calls, _ := perks.seen(); calls != 1 {
		t.Errorf("the conditional step ran %d times, want once for a gold customer", calls)
	}
}

// What a step does when it fails is the difference between a degraded answer
// and no answer, and it is declared per step because both are right in
// different places: a missing balance may be tolerable, a missing customer is
// not.

func failingSteps(t *testing.T, onError string, def interface{}) (*FlowHandler, *stepConnector) {
	t.Helper()
	failing := &stepConnector{name: "billing", err: errors.New("connection refused")}
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "balance", Connector: "billing", OnError: onError, Default: def,
		ConnectorParams: map[string]interface{}{"operation": "GET /balance"},
	}}, map[string]connector.Connector{"billing": failing})
	return h, failing
}

func TestAFailingStepStopsTheFlowUnlessItSaysOtherwise(t *testing.T) {
	// The default has to be the strict one: a flow that silently drops a field
	// because a dependency was down is worse than one that fails.
	h, _ := failingSteps(t, "", nil)
	_, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("a failing step was ignored")
	}
	if !strings.Contains(err.Error(), "balance") {
		t.Errorf("error = %q, want it to name the step", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want the reason underneath", err)
	}
}

func TestAFailingStepCanBeSkipped(t *testing.T) {
	h, _ := failingSteps(t, "skip", nil)
	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("a step declared skippable failed the flow: %v", err)
	}
	value, present := results["balance"]
	if !present {
		t.Error("the skipped step left its name unbound")
	}
	if value != nil {
		t.Errorf("balance = %v, want nothing", value)
	}
}

func TestAFailingStepCanFallBackToADefault(t *testing.T) {
	// A cached or conservative value in place of a dependency that is down,
	// which is what keeps a response useful rather than empty.
	h, _ := failingSteps(t, "default", map[string]interface{}{"balance": 0, "stale": true})
	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	value, ok := results["balance"].(map[string]interface{})
	if !ok || value["stale"] != true {
		t.Errorf("balance = %#v, want the declared default", results["balance"])
	}
}

func TestAStepNamingAConnectorThatDoesNotExistIsReported(t *testing.T) {
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "balance", Connector: "typo",
		ConnectorParams: map[string]interface{}{"operation": "GET /balance"},
	}}, nil)

	_, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("a step naming a connector that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "balance") || !strings.Contains(err.Error(), "typo") {
		t.Errorf("error = %q, want it to name the step and the connector", err)
	}
}

func TestAMissingConnectorCanAlsoBeSkipped(t *testing.T) {
	// Consistent with a failing call: what the step says to do on error applies
	// however the step failed.
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "balance", Connector: "typo", OnError: "skip",
		ConnectorParams: map[string]interface{}{"operation": "GET /balance"},
	}}, nil)

	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	if _, present := results["balance"]; !present {
		t.Error("the step left its name unbound")
	}
}

func TestAParameterThatIsNotAnExpressionIsPassedThrough(t *testing.T) {
	// Most parameters are constants, and treating one as an expression would
	// turn a table name into a failed lookup.
	db := &stepConnector{name: "db", rows: []map[string]interface{}{{"id": "1"}}}
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "orders", Connector: "db",
		ConnectorParams: map[string]interface{}{
			"query": "SELECT * FROM orders WHERE status = :status AND region = :region",
			"params": map[string]interface{}{
				"status": "shipped",
				"region": "input.region",
				"limit":  100,
			},
		},
	}}, map[string]connector.Connector{"db": db})

	_, err := h.executeSteps(context.Background(), map[string]interface{}{"region": "nz"})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	_, params := db.seen()
	if len(params) != 1 {
		t.Fatalf("the step was called %d times", len(params))
	}
	if params[0]["status"] != "shipped" {
		t.Errorf("status = %v, want the constant as written", params[0]["status"])
	}
	if params[0]["region"] != "nz" {
		t.Errorf("region = %v, want the expression evaluated", params[0]["region"])
	}
	if params[0]["limit"] != 100 {
		t.Errorf("limit = %v, want the number as written", params[0]["limit"])
	}
}

func TestAParameterExpressionThatCannotBeEvaluatedIsReported(t *testing.T) {
	// Naming a step that does not exist, or a field that is not there, is a
	// configuration mistake and has to name where it is.
	db := &stepConnector{name: "db"}
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "orders", Connector: "db",
		ConnectorParams: map[string]interface{}{
			"query":  "SELECT 1",
			"params": map[string]interface{}{"id": "step.absent.id"},
		},
	}}, map[string]connector.Connector{"db": db})

	_, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("a parameter referring to a step that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "orders") || !strings.Contains(err.Error(), "id") {
		t.Errorf("error = %q, want it to name the step and the parameter", err)
	}
}

func TestEveryStepRunsOnce(t *testing.T) {
	makeConn := func(name string) *stepConnector {
		return &stepConnector{name: name, call: map[string]interface{}{"n": name}}
	}
	first, second, third := makeConn("first"), makeConn("second"), makeConn("third")

	h := stepHandler(t, []*flow.StepConfig{
		{Name: "a", Connector: "first", ConnectorParams: map[string]interface{}{"operation": "GET /a"}},
		{Name: "b", Connector: "second", ConnectorParams: map[string]interface{}{"operation": "GET /b"}},
		{Name: "c", Connector: "third", ConnectorParams: map[string]interface{}{"operation": "GET /c"}},
	}, map[string]connector.Connector{"first": first, "second": second, "third": third})

	results, err := h.executeSteps(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	for name, conn := range map[string]*stepConnector{"a": first, "b": second, "c": third} {
		if calls, _ := conn.seen(); calls != 1 {
			t.Errorf("step %s ran %d times, want once", name, calls)
		}
		if _, present := results[name]; !present {
			t.Errorf("step %s left no result behind", name)
		}
	}
}

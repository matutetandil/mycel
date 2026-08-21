package statemachine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// mockConnector implements connector.ReadWriter and Caller for state machine tests.
type mockConnector struct {
	name       string
	rows       []map[string]interface{} // rows returned by Read
	writes     []*connector.Data
	readErr    error
	writeErr   error
	callResult interface{}
	callErr    error
	callOps    []string
}

func (m *mockConnector) Name() string                      { return m.name }
func (m *mockConnector) Type() string                      { return "mock" }
func (m *mockConnector) Connect(ctx context.Context) error { return nil }
func (m *mockConnector) Close(ctx context.Context) error   { return nil }
func (m *mockConnector) Health(ctx context.Context) error  { return nil }

func (m *mockConnector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	return &connector.Result{Rows: m.rows}, nil
}

func (m *mockConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	if m.writeErr != nil {
		return nil, m.writeErr
	}
	m.writes = append(m.writes, data)
	return &connector.Result{Affected: 1}, nil
}

func (m *mockConnector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	m.callOps = append(m.callOps, operation)
	if m.callErr != nil {
		return nil, m.callErr
	}
	if m.callResult != nil {
		return m.callResult, nil
	}
	return map[string]interface{}{"ok": true}, nil
}

// mockRegistry implements ConnectorGetter with List support.
type mockRegistry struct {
	connectors map[string]connector.Connector
}

func (r *mockRegistry) Get(name string) (connector.Connector, error) {
	c, ok := r.connectors[name]
	if !ok {
		return nil, fmt.Errorf("connector %q not found", name)
	}
	return c, nil
}

func (r *mockRegistry) List() []string {
	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}

func newTestMachine() *Config {
	return &Config{
		Name:    "order_status",
		Initial: "pending",
		States: map[string]*StateConfig{
			"pending": {
				Name: "pending",
				Transitions: map[string]*TransitionConfig{
					"pay":    {Event: "pay", TransitionTo: "paid"},
					"cancel": {Event: "cancel", TransitionTo: "cancelled"},
				},
			},
			"paid": {
				Name: "paid",
				Transitions: map[string]*TransitionConfig{
					"ship": {Event: "ship", TransitionTo: "shipped",
						Guard: "input.tracking_number != ''"},
					"refund": {Event: "refund", TransitionTo: "refunded"},
				},
			},
			"shipped": {
				Name: "shipped",
				Transitions: map[string]*TransitionConfig{
					"deliver": {Event: "deliver", TransitionTo: "delivered"},
				},
			},
			"delivered": {Name: "delivered", Final: true},
			"cancelled": {Name: "cancelled", Final: true},
			"refunded":  {Name: "refunded", Final: true},
		},
	}
}

func TestEngine_ValidTransition(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "pending"}},
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	result, err := engine.Transition(context.Background(), "order_status", "orders", "1", "pay", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PreviousState != "pending" {
		t.Errorf("expected previous state pending, got %s", result.PreviousState)
	}
	if result.CurrentState != "paid" {
		t.Errorf("expected current state paid, got %s", result.CurrentState)
	}
	if result.Event != "pay" {
		t.Errorf("expected event pay, got %s", result.Event)
	}
	// Should have written new status
	if len(db.writes) != 1 {
		t.Errorf("expected 1 write (status update), got %d", len(db.writes))
	}
}

func TestEngine_InitialState(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: nil, // entity not found → initial state
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	result, err := engine.Transition(context.Background(), "order_status", "orders", "new", "pay", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PreviousState != "pending" {
		t.Errorf("expected previous state pending (initial), got %s", result.PreviousState)
	}
	if result.CurrentState != "paid" {
		t.Errorf("expected current state paid, got %s", result.CurrentState)
	}
}

func TestEngine_InvalidEvent(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "pending"}},
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	_, err := engine.Transition(context.Background(), "order_status", "orders", "1", "ship", nil)
	if err == nil {
		t.Fatal("expected error for invalid event")
	}
}

func TestEngine_FinalState(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "delivered"}},
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	_, err := engine.Transition(context.Background(), "order_status", "orders", "1", "ship", nil)
	if err == nil {
		t.Fatal("expected error for final state transition")
	}
}

func TestEngine_GuardPasses(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "paid"}},
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	data := map[string]interface{}{"tracking_number": "TRK123"}
	result, err := engine.Transition(context.Background(), "order_status", "orders", "1", "ship", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CurrentState != "shipped" {
		t.Errorf("expected shipped, got %s", result.CurrentState)
	}
}

func TestEngine_GuardRejects(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "paid"}},
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	data := map[string]interface{}{"tracking_number": ""}
	_, err := engine.Transition(context.Background(), "order_status", "orders", "1", "ship", data)
	if err == nil {
		t.Fatal("expected guard rejection error")
	}
}

func TestEngine_MachineNotFound(t *testing.T) {
	reg := &mockRegistry{connectors: map[string]connector.Connector{}}

	engine := NewEngine(reg)

	_, err := engine.Transition(context.Background(), "nonexistent", "orders", "1", "pay", nil)
	if err == nil {
		t.Fatal("expected error for missing machine")
	}
}

func TestEngine_TransitionWithAction(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "paid"}},
	}
	notifications := &mockConnector{name: "notifications"}
	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"db":            db,
		"notifications": notifications,
	}}

	engine := NewEngine(reg)
	machine := &Config{
		Name:    "order_status",
		Initial: "pending",
		States: map[string]*StateConfig{
			"paid": {
				Name: "paid",
				Transitions: map[string]*TransitionConfig{
					"ship": {
						Event:        "ship",
						TransitionTo: "shipped",
						Action: &ActionConfig{
							Connector: "notifications",
							Operation: "POST /send",
							Body:      map[string]interface{}{"template": "order_shipped"},
						},
					},
				},
			},
			"shipped": {Name: "shipped", Final: true},
		},
	}
	engine.Register(machine)

	result, err := engine.Transition(context.Background(), "order_status", "orders", "1", "ship", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CurrentState != "shipped" {
		t.Errorf("expected shipped, got %s", result.CurrentState)
	}
	if len(notifications.callOps) != 1 {
		t.Errorf("expected 1 notification call, got %d", len(notifications.callOps))
	}
}

func TestEngine_MultipleTransitions(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "pending"}},
	}
	reg := &mockRegistry{connectors: map[string]connector.Connector{"db": db}}

	engine := NewEngine(reg)
	engine.Register(newTestMachine())

	// pending → paid
	result, err := engine.Transition(context.Background(), "order_status", "orders", "1", "pay", nil)
	if err != nil {
		t.Fatalf("first transition error: %v", err)
	}
	if result.CurrentState != "paid" {
		t.Errorf("expected paid after first transition, got %s", result.CurrentState)
	}

	// Update mock to reflect new state
	db.rows = []map[string]interface{}{{"id": "1", "status": "paid"}}

	// paid → shipped
	data := map[string]interface{}{"tracking_number": "TRK456"}
	result, err = engine.Transition(context.Background(), "order_status", "orders", "1", "ship", data)
	if err != nil {
		t.Fatalf("second transition error: %v", err)
	}
	if result.CurrentState != "shipped" {
		t.Errorf("expected shipped after second transition, got %s", result.CurrentState)
	}
}

func TestEngine_ActionFailure(t *testing.T) {
	db := &mockConnector{
		name: "db",
		rows: []map[string]interface{}{{"id": "1", "status": "paid"}},
	}
	brokenSvc := &mockConnector{name: "broken", callErr: fmt.Errorf("service down")}
	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"db":     db,
		"broken": brokenSvc,
	}}

	engine := NewEngine(reg)
	machine := &Config{
		Name:    "test",
		Initial: "start",
		States: map[string]*StateConfig{
			"paid": {
				Name: "paid",
				Transitions: map[string]*TransitionConfig{
					"process": {
						Event:        "process",
						TransitionTo: "processing",
						Action: &ActionConfig{
							Connector: "broken",
							Operation: "POST /process",
						},
					},
				},
			},
			"processing": {Name: "processing", Final: true},
		},
	}
	engine.Register(machine)

	_, err := engine.Transition(context.Background(), "test", "orders", "1", "process", nil)
	if err == nil {
		t.Fatal("expected error from broken action")
	}
	// State should NOT have been updated (action failed before write)
	if len(db.writes) != 0 {
		t.Errorf("expected 0 writes (action failed), got %d", len(db.writes))
	}
}

// Which connector holds the entity.
//
// Without a name the engine tries every connector in turn and uses the first
// that does not fail, and the order is whatever map iteration gives. In a
// service with a database and a message queue the queue accepted the write
// happily: the new state was published to a topic and the row it was meant for
// went untouched, with the transition reporting success.

func TestTheEntityIsReadAndWrittenWhereTheFlowSaysItIs(t *testing.T) {
	orders := &mockConnector{name: "orders_db", rows: []map[string]interface{}{{"status": "pending"}}}
	queue := &mockConnector{name: "events_queue"}

	engine := NewEngine(&mockRegistry{connectors: map[string]connector.Connector{
		"orders_db":    orders,
		"events_queue": queue,
	}})
	engine.Register(newTestMachine())

	result, err := engine.TransitionOn(context.Background(), "orders_db",
		"order_status", "orders", "order-1", "pay", map[string]interface{}{})
	if err != nil {
		t.Fatalf("TransitionOn: %v", err)
	}
	if result.CurrentState != "paid" {
		t.Errorf("state = %q", result.CurrentState)
	}

	if len(orders.writes) != 1 {
		t.Errorf("%d writes to the database holding the entity, want one", len(orders.writes))
	}
	if len(queue.writes) != 0 {
		t.Errorf("the new state was written to %q, which does not hold the entity", queue.name)
	}
}

func TestAConnectorThatDoesNotHoldTheEntityIsNotFallenBackOn(t *testing.T) {
	// If the named connector cannot take the write, that is a failure worth
	// reporting — not a reason to write the state somewhere else.
	orders := &mockConnector{
		name:     "orders_db",
		rows:     []map[string]interface{}{{"status": "pending"}},
		writeErr: errors.New("connection refused"),
	}
	queue := &mockConnector{name: "events_queue"}

	engine := NewEngine(&mockRegistry{connectors: map[string]connector.Connector{
		"orders_db":    orders,
		"events_queue": queue,
	}})
	engine.Register(newTestMachine())

	_, err := engine.TransitionOn(context.Background(), "orders_db",
		"order_status", "orders", "order-1", "pay", map[string]interface{}{})
	if err == nil {
		t.Fatal("the transition reported success although the state was not written")
	}
	if len(queue.writes) != 0 {
		t.Errorf("the state was written to %q instead", queue.name)
	}
}

func TestWithNoConnectorNamedEveryOneIsStillTried(t *testing.T) {
	// The older form, which flows written before the attribute existed rely on.
	orders := &mockConnector{name: "orders_db", rows: []map[string]interface{}{{"status": "pending"}}}

	engine := NewEngine(&mockRegistry{connectors: map[string]connector.Connector{
		"orders_db": orders,
	}})
	engine.Register(newTestMachine())

	if _, err := engine.Transition(context.Background(),
		"order_status", "orders", "order-1", "pay", map[string]interface{}{}); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if len(orders.writes) != 1 {
		t.Errorf("%d writes, want one", len(orders.writes))
	}
}

package runtime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/connector/database/sqlite"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/statemachine"
	"github.com/matutetandil/mycel/v2/internal/transform"
)

// A state machine is the shape of a thing's life — an order goes pending →
// paid → shipped and never backwards, and an event that does not belong to the
// state it arrives in is refused rather than applied. A flow drives one with a
// state_transition block, and nothing covered it: the entity's current state is
// read from its own row, so a mistake here writes the wrong state to a real
// record.

func orderMachine(t *testing.T) (*FlowHandler, *sqlite.Connector) {
	t.Helper()

	db := sqlite.New("db", ":memory:", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := db.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })

	if err := db.Exec(context.Background(),
		`CREATE TABLE orders (id TEXT PRIMARY KEY, status TEXT, paid_at TEXT)`); err != nil {
		t.Fatalf("creating the table: %v", err)
	}
	if err := db.Exec(context.Background(),
		`INSERT INTO orders (id, status) VALUES ('order-1', 'pending')`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	registry := connector.NewRegistry()
	registry.Replace("db", db)

	engine := statemachine.NewEngine(registry)
	engine.Register(&statemachine.Config{
		Name:    "order_lifecycle",
		Initial: "pending",
		States: map[string]*statemachine.StateConfig{
			"pending": {Name: "pending", Transitions: map[string]*statemachine.TransitionConfig{
				"pay":    {Event: "pay", TransitionTo: "paid"},
				"cancel": {Event: "cancel", TransitionTo: "cancelled"},
			}},
			"paid": {Name: "paid", Transitions: map[string]*statemachine.TransitionConfig{
				"ship": {Event: "ship", TransitionTo: "shipped"},
			}},
			"shipped":   {Name: "shipped", Final: true},
			"cancelled": {Name: "cancelled", Final: true},
		},
	})

	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	return &FlowHandler{
		Config: &flow.Config{
			Name: "advance_order",
			From: &flow.FromConfig{Connector: "api"},
			StateTransition: &flow.StateTransitionConfig{
				Machine: "order_lifecycle",
				Entity:  "orders",
				ID:      "input.order_id",
				Event:   "input.event",
			},
		},
		Connectors:         registry,
		Transformer:        transformer,
		StateMachineEngine: engine,
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, db
}

func stateOf(t *testing.T, db *sqlite.Connector, id string) string {
	t.Helper()
	result, err := db.Read(context.Background(), connector.Query{
		Target: "orders", Filters: map[string]interface{}{"id": id},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("%d rows for %s", len(result.Rows), id)
	}
	// The state lives in the entity's own status column, which is what the
	// documentation says and what the engine reads and writes.
	state, _ := result.Rows[0]["status"].(string)
	return state
}

func TestAnEventMovesTheThingToItsNextState(t *testing.T) {
	handler, db := orderMachine(t)

	if _, err := handler.executeStateTransition(context.Background(),
		map[string]interface{}{"order_id": "order-1", "event": "pay"}); err != nil {
		t.Fatalf("executeStateTransition: %v", err)
	}

	if state := stateOf(t, db, "order-1"); state != "paid" {
		t.Errorf("state = %q, want the one the event leads to", state)
	}
}

func TestAnEventThatDoesNotBelongToTheStateIsRefused(t *testing.T) {
	// Shipping something nobody paid for. The order stays where it was, which
	// is the point of having a machine rather than an update statement.
	handler, db := orderMachine(t)

	_, err := handler.executeStateTransition(context.Background(),
		map[string]interface{}{"order_id": "order-1", "event": "ship"})
	if err == nil {
		t.Fatal("an order was shipped straight from pending")
	}
	if state := stateOf(t, db, "order-1"); state != "pending" {
		t.Errorf("state = %q, want it untouched", state)
	}
}

func TestAThingThatHasFinishedDoesNotMoveAgain(t *testing.T) {
	handler, db := orderMachine(t)
	ctx := context.Background()

	if _, err := handler.executeStateTransition(ctx,
		map[string]interface{}{"order_id": "order-1", "event": "cancel"}); err != nil {
		t.Fatalf("executeStateTransition: %v", err)
	}
	if state := stateOf(t, db, "order-1"); state != "cancelled" {
		t.Fatalf("state = %q", state)
	}

	_, err := handler.executeStateTransition(ctx,
		map[string]interface{}{"order_id": "order-1", "event": "pay"})
	if err == nil {
		t.Error("a cancelled order was paid")
	}
	if state := stateOf(t, db, "order-1"); state != "cancelled" {
		t.Errorf("state = %q, want it to stay finished", state)
	}
}

func TestAnEventNobodyDefinedIsReported(t *testing.T) {
	handler, _ := orderMachine(t)

	_, err := handler.executeStateTransition(context.Background(),
		map[string]interface{}{"order_id": "order-1", "event": "refund"})
	if err == nil {
		t.Fatal("an event the machine does not have was applied")
	}
	if !strings.Contains(err.Error(), "refund") {
		t.Errorf("error = %q, want it to name the event", err)
	}
}

func TestAMachineNobodyRegisteredIsReported(t *testing.T) {
	handler, _ := orderMachine(t)
	handler.Config.StateTransition.Machine = "invoice_lifecycle"

	_, err := handler.executeStateTransition(context.Background(),
		map[string]interface{}{"order_id": "order-1", "event": "pay"})
	if err == nil {
		t.Fatal("a machine nobody defined was driven")
	}
	if !strings.Contains(err.Error(), "invoice_lifecycle") {
		t.Errorf("error = %q, want it to name the machine", err)
	}
}

func TestATransitionWithNoEntityToApplyItToIsReported(t *testing.T) {
	// The identifier and the event are expressions over the message, and both
	// name a field somebody could get wrong.
	handler, _ := orderMachine(t)

	_, err := handler.executeStateTransition(context.Background(),
		map[string]interface{}{"event": "pay"})
	if err == nil {
		t.Fatal("a transition ran with no entity to apply it to")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Errorf("error = %q, want it to say which expression failed", err)
	}
}

func TestAnEventThatIsNotAWordIsReported(t *testing.T) {
	handler, _ := orderMachine(t)
	handler.Config.StateTransition.Event = "input.count"

	_, err := handler.executeStateTransition(context.Background(),
		map[string]interface{}{"order_id": "order-1", "count": 42})
	if err == nil {
		t.Error("a number was applied as an event name")
	}
}

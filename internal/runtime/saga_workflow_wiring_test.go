package runtime

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/saga"
	"github.com/matutetandil/mycel/v3/internal/workflow"
)

// A saga with a delay or an await step is a long-running workflow: it pauses,
// its state is persisted, and something wakes it later over the workflow api.
// What decides that at runtime is the handler holding a workflow engine —
// `if h.WorkflowEngine != nil && NeedsPersistence(...)`.
//
// Sagas were registered before the engine was built, so every handler captured
// a nil engine and that check was false for all of them. Each saga ran straight
// through the synchronous executor instead: it never paused, nothing was
// persisted, no workflow id came back, and the endpoints that drive workflows
// had nothing to serve. The feature had documentation and an example and no way
// to happen.

func TestASagaHandlerIsGivenTheEngineThatExistsWhenItIsRegistered(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := workflow.NewSQLStore(db, workflow.DialectSQLite, "workflow_instances")
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	registry := connector.NewRegistry()
	registry.Replace("api", &stepConnector{name: "api"})

	r := &Runtime{
		connectors: registry,
		flows:      NewFlowRegistry(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: &parser.Configuration{
			Sagas: []*saga.Config{{
				Name: "approval",
				From: &saga.FromConfig{Connector: "api", Operation: "POST /approval"},
				Steps: []*saga.StepConfig{
					{Name: "wait", Await: "approved"},
				},
			}},
		},
		// The engine exists before the sagas are registered, which is the
		// order Start has to use.
		workflowEngine: workflow.NewEngine(store, saga.NewExecutor(registry),
			slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	if err := r.registerSagas(); err != nil {
		t.Fatalf("registerSagas: %v", err)
	}

	handler, ok := r.flows.Get("approval")
	if !ok {
		t.Fatal("the saga was not registered")
	}
	if handler.WorkflowEngine == nil {
		t.Fatal("the saga handler has no workflow engine, so a saga that waits would run straight through")
	}
	if !workflow.NeedsPersistence(handler.SagaConfig) {
		t.Error("a saga with an await step was not recognised as one that has to be persisted")
	}
}

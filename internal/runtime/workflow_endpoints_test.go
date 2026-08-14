package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/saga"
	"github.com/matutetandil/mycel/v2/internal/workflow"
)

// A long-running workflow outlives the request that started it, so the only way
// to see one, wake it or stop it is over these endpoints. They are what an
// operator reaches for when a workflow has been waiting on an event for a day.

func workflowRuntime(t *testing.T) (*workflow.SQLStore, *workflow.Engine, *http.ServeMux) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := workflow.NewSQLStore(db, workflow.DialectSQLite, "workflow_instances")
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	engine := workflow.NewEngine(store, saga.NewExecutor(connector.NewRegistry()),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := &Runtime{
		workflowEngine: engine,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	r.registerWorkflowEndpoints(mux)
	return store, engine, mux
}

// paused stores an instance waiting on an event, which is the state every one
// of these endpoints exists for.
func paused(t *testing.T, store *workflow.SQLStore, id, event string) {
	t.Helper()
	inst := &workflow.Instance{
		ID:         id,
		SagaName:   "checkout",
		Status:     workflow.StatusPaused,
		AwaitEvent: event,
		Input:      map[string]interface{}{"order_id": "order-1"},
	}
	if err := store.Save(context.Background(), inst); err != nil {
		t.Fatalf("saving the instance: %v", err)
	}
}

func TestAWorkflowCanBeLookedUpByItsIdentifier(t *testing.T) {
	store, _, mux := workflowRuntime(t)
	paused(t, store, "wf-1", "payment_received")

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/workflows/wf-1", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var instance map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &instance); err != nil {
		t.Fatalf("the answer was not JSON: %v", err)
	}
	// What an operator came to find out: what it is waiting for.
	if instance["await_event"] != "payment_received" {
		t.Errorf("instance = %v", instance)
	}
}

func TestAWorkflowNobodyStartedIsNotFound(t *testing.T) {
	_, _, mux := workflowRuntime(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/workflows/wf-absent", nil))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
}

func TestASignalCarriesItsDataToTheWorkflow(t *testing.T) {
	store, engine, mux := workflowRuntime(t)
	paused(t, store, "wf-2", "payment_received")

	body := strings.NewReader(`{"amount": 4200, "reference": "pay-99"}`)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/workflows/wf-2/signal/payment_received", body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}

	instance, err := engine.GetInstance(context.Background(), "wf-2")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if instance.SignalData["reference"] != "pay-99" {
		t.Errorf("the workflow resumed with %v, want the data the signal carried", instance.SignalData)
	}
}

func TestASignalWhoseBodyIsNotJSONIsRefused(t *testing.T) {
	// The body was decoded and the error thrown away, so a malformed payload
	// woke the workflow with no data at all — it carried on down a branch that
	// reads a field that is not there, and the caller was told it worked.
	store, engine, mux := workflowRuntime(t)
	paused(t, store, "wf-3", "payment_received")

	body := strings.NewReader(`{"amount": 4200,`)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/workflows/wf-3/signal/payment_received", body))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}

	instance, err := engine.GetInstance(context.Background(), "wf-3")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if instance.Status != workflow.StatusPaused {
		t.Errorf("the workflow was woken by a signal that was refused: %s", instance.Status)
	}
}

func TestASignalWithNoBodyIsStillASignal(t *testing.T) {
	// Plenty of events carry nothing but their name.
	store, _, mux := workflowRuntime(t)
	paused(t, store, "wf-4", "approved")

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/workflows/wf-4/signal/approved", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
}

func TestASignalForAnEventTheWorkflowIsNotWaitingOnIsRefused(t *testing.T) {
	store, _, mux := workflowRuntime(t)
	paused(t, store, "wf-5", "payment_received")

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/workflows/wf-5/signal/shipped", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "payment_received") {
		t.Errorf("body = %q, want it to say what the workflow is waiting for", recorder.Body)
	}
}

func TestAWorkflowCanBeStopped(t *testing.T) {
	store, engine, mux := workflowRuntime(t)
	paused(t, store, "wf-6", "payment_received")

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/workflows/wf-6/cancel", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}

	instance, err := engine.GetInstance(context.Background(), "wf-6")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if instance.Status != workflow.StatusCancelled {
		t.Errorf("status = %s, want it cancelled", instance.Status)
	}
}

func TestWithNoWorkflowEngineTheEndpointsAreNotThere(t *testing.T) {
	// A service with no workflows should not answer for them: a 404 from the
	// mux says the feature is off, where a 500 would say it is broken.
	r := &Runtime{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	r.registerWorkflowEndpoints(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/workflows/wf-1", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
}

package runtime

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/internal/workflow"
)

// The workflow endpoints wake a paused workflow with data the caller chooses
// and cancel one that is running. They used to sit on the admin server — the
// port that carries health and metrics, read-only and unauthenticated by
// design, mounted the moment a workflow engine was configured — so anything
// that could reach that port could approve the loan a workflow was waiting on.
//
// They listen on their own port now, and a caller has to say who they are.

func workflowAPI(t *testing.T, auth map[string]interface{}) (*Runtime, *workflow.SQLStore, http.Handler) {
	t.Helper()
	store, engine, _ := workflowRuntime(t)

	r := &Runtime{
		workflowEngine: engine,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: &parser.Configuration{
			ServiceConfig: &parser.ServiceConfig{
				Workflow: &parser.WorkflowConfig{
					Storage: "db",
					API:     &parser.WorkflowAPIConfig{Port: 0, Auth: auth},
				},
			},
		},
	}

	handler, err := r.workflowAPIHandler()
	if err != nil {
		t.Fatalf("workflowAPIHandler: %v", err)
	}
	return r, store, handler
}

func TestTheWorkflowEndpointsAreNotOnTheAdminServer(t *testing.T) {
	// The whole point, against the admin server a service actually starts: a
	// workflow engine must not put a mutating interface on the port that
	// serves health and metrics.
	dir := t.TempDir()
	config := `
service {
  name       = "worker"
  admin_port = 19097

  workflow {
    storage = "db"
  }
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
`
	if err := os.WriteFile(filepath.Join(dir, "config.mycel"), []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, dir)
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	defer rt.Shutdown()
	waitForServer(t, 19097)

	for _, path := range []string{
		"/workflows/wf-1",
		"/workflows/wf-1/signal/approved",
		"/workflows/wf-1/cancel",
	} {
		resp, err := http.Get("http://localhost:19097" + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d on the admin server", path, resp.StatusCode)
		}
	}
}

func TestACallerWithNoCredentialsIsTurnedAway(t *testing.T) {
	_, store, handler := workflowAPI(t, map[string]interface{}{
		"type": "api_key", "keys": []interface{}{"the-key"},
	})
	paused(t, store, "wf-1", "payment_received")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("POST", "/workflows/wf-1/signal/payment_received", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	// And nothing happened to the workflow.
	instance, err := workflowInstance(t, store, "wf-1")
	if err != nil {
		t.Fatalf("reading the workflow: %v", err)
	}
	if instance.Status != workflow.StatusPaused {
		t.Errorf("the workflow was woken by a caller who was turned away: %s", instance.Status)
	}
}

func TestACallerWithTheKeyIsServed(t *testing.T) {
	_, store, handler := workflowAPI(t, map[string]interface{}{
		"type": "api_key", "keys": []interface{}{"the-key"},
	})
	paused(t, store, "wf-2", "payment_received")

	request := httptest.NewRequest("GET", "/workflows/wf-2", nil)
	request.Header.Set("X-API-Key", "the-key")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "payment_received") {
		t.Errorf("body = %s", recorder.Body)
	}
}

func TestAKeyNobodyIssuedIsTurnedAway(t *testing.T) {
	_, store, handler := workflowAPI(t, map[string]interface{}{
		"type": "api_key", "keys": []interface{}{"the-key"},
	})
	paused(t, store, "wf-3", "payment_received")

	request := httptest.NewRequest("POST", "/workflows/wf-3/cancel", nil)
	request.Header.Set("X-API-Key", "a-key-nobody-issued")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", recorder.Code)
	}
}

func TestAPasswordWorksJustAsWell(t *testing.T) {
	// The same auth block a connector takes, so basic is available without
	// anything being written twice.
	_, store, handler := workflowAPI(t, map[string]interface{}{
		"type":  "basic",
		"users": map[string]interface{}{"ops": "s3cret"},
	})
	paused(t, store, "wf-4", "approved")

	request := httptest.NewRequest("GET", "/workflows/wf-4", nil)
	request.Header.Set("Authorization", "Basic "+
		base64.StdEncoding.EncodeToString([]byte("ops:s3cret")))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}

	wrong := httptest.NewRequest("GET", "/workflows/wf-4", nil)
	wrong.Header.Set("Authorization", "Basic "+
		base64.StdEncoding.EncodeToString([]byte("ops:guessed")))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, wrong)
	if recorder.Code == http.StatusOK {
		t.Error("the wrong password was accepted")
	}
}

func TestAnApiWithNothingToCheckAgainstDoesNotStart(t *testing.T) {
	// The parser refuses this too. Here because the runtime must not serve
	// the endpoints unauthenticated if it is ever handed such a configuration
	// another way — a plugin, a test, a future loader.
	_, engine, _ := workflowRuntime(t)
	r := &Runtime{
		workflowEngine: engine,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: &parser.Configuration{
			ServiceConfig: &parser.ServiceConfig{
				Workflow: &parser.WorkflowConfig{
					API: &parser.WorkflowAPIConfig{Port: 9091},
				},
			},
		},
	}

	if _, err := r.workflowAPIHandler(); err == nil {
		t.Error("the workflow endpoints were served with nothing to check callers against")
	}
}

func TestWithNoApiConfiguredNothingListens(t *testing.T) {
	_, engine, _ := workflowRuntime(t)
	r := &Runtime{
		workflowEngine: engine,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: &parser.Configuration{
			ServiceConfig: &parser.ServiceConfig{
				Workflow: &parser.WorkflowConfig{Storage: "db"},
			},
		},
	}

	handler, err := r.workflowAPIHandler()
	if err != nil {
		t.Fatalf("workflowAPIHandler: %v", err)
	}
	if handler != nil {
		t.Error("something was served although no api was configured")
	}
}

func workflowInstance(t *testing.T, store *workflow.SQLStore, id string) (*workflow.Instance, error) {
	t.Helper()
	return store.Get(context.Background(), id)
}

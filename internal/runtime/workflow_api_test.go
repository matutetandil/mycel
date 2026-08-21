package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
)

// Who may wake a running workflow.
//
// These three endpoints are not read-only: signalling one wakes it with data
// the caller chooses, and cancelling stops it mid-flight. They used to be
// mounted on the admin port, which carries health and metrics and is
// unauthenticated by design — so anything on the network could cancel a
// workflow. They have their own port now, are served only when asked for, and
// are not served at all without something to check callers against.
//
// The endpoints themselves are covered elsewhere. What is covered here is the
// gate in front of them, which is the part a mistake in is expensive.

func runtimeWithWorkflowAPI(t *testing.T, api *parser.WorkflowAPIConfig) *Runtime {
	t.Helper()

	store, engine, _ := workflowRuntime(t)
	_ = store

	return &Runtime{
		workflowEngine: engine,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: &parser.Configuration{
			ServiceConfig: &parser.ServiceConfig{
				Workflow: &parser.WorkflowConfig{API: api},
			},
		},
	}
}

func TestTheWorkflowEndpointsAreNotServedWithoutAWayToCheckCallers(t *testing.T) {
	for name, api := range map[string]*parser.WorkflowAPIConfig{
		"no auth block at all":       {Port: 9091},
		"an auth block with no type": {Port: 9091, Auth: map[string]interface{}{"header": "X-API-Key"}},
	} {
		t.Run(name, func(t *testing.T) {
			handler, err := runtimeWithWorkflowAPI(t, api).workflowAPIHandler()
			if err == nil {
				t.Fatal("the endpoints were served with nothing checking callers")
			}
			if handler != nil {
				t.Error("a handler came back alongside the error")
			}
			// The message has to say why, since the alternative reading is
			// that the feature is broken rather than refused.
			if !strings.Contains(err.Error(), "auth") {
				t.Errorf("the error does not mention auth: %v", err)
			}
		})
	}
}

func TestAnAuthTypeNobodyImplementsIsRefused(t *testing.T) {
	_, err := runtimeWithWorkflowAPI(t, &parser.WorkflowAPIConfig{
		Port: 9091,
		Auth: map[string]interface{}{"type": "oauth2"},
	}).workflowAPIHandler()

	if err == nil {
		t.Fatal("an auth type this cannot honour was accepted")
	}
	if !strings.Contains(err.Error(), "jwt") {
		t.Errorf("the error does not say which types work: %v", err)
	}
}

func TestNoAPIBlockMeansNoEndpoints(t *testing.T) {
	// The default. A service with workflows but no api block exposes nothing.
	handler, err := runtimeWithWorkflowAPI(t, nil).workflowAPIHandler()
	if err != nil {
		t.Fatalf("a service with no api block errored: %v", err)
	}
	if handler != nil {
		t.Error("a handler was built for a service that asked for none")
	}
}

func TestWithNoEngineThereIsNothingToServe(t *testing.T) {
	r := &Runtime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: &parser.Configuration{
			ServiceConfig: &parser.ServiceConfig{
				Workflow: &parser.WorkflowConfig{API: &parser.WorkflowAPIConfig{
					Port: 9091,
					Auth: map[string]interface{}{"type": "api_key", "keys": []interface{}{"secret"}},
				}},
			},
		},
	}

	handler, err := r.workflowAPIHandler()
	if err != nil {
		t.Fatalf("errored: %v", err)
	}
	if handler != nil {
		t.Error("a handler was built with no workflow engine behind it")
	}
}

func TestACallerWithoutTheKeyIsTurnedAway(t *testing.T) {
	handler, err := runtimeWithWorkflowAPI(t, &parser.WorkflowAPIConfig{
		Port: 9091,
		Auth: map[string]interface{}{
			"type":   "api_key",
			"header": "X-API-Key",
			"keys":   []interface{}{"the-right-key"},
		},
	}).workflowAPIHandler()
	if err != nil {
		t.Fatalf("workflowAPIHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("no handler")
	}

	for name, tc := range map[string]struct {
		key      string
		wantAuth bool
	}{
		"no key at all": {"", false},
		"the wrong key": {"a-guess", false},
		"the right key": {"the-right-key", true},
	} {
		t.Run(name, func(t *testing.T) {
			// Cancelling is the one that costs the most to get wrong.
			req := httptest.NewRequest(http.MethodPost, "/workflows/wf-1/cancel", nil)
			if tc.key != "" {
				req.Header.Set("X-API-Key", tc.key)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			refused := rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden
			if tc.wantAuth && refused {
				t.Errorf("a caller with the right key was refused: %d %s", rec.Code, rec.Body.String())
			}
			if !tc.wantAuth && !refused {
				t.Errorf("a caller without it reached the endpoint: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTheListenerComesUpAndGoesDown(t *testing.T) {
	// On its own port, and closed with the rest of the service: a listener
	// left behind holds the port against the next start.
	r := runtimeWithWorkflowAPI(t, &parser.WorkflowAPIConfig{
		Host: "127.0.0.1",
		Port: 0, // whatever is free
		Auth: map[string]interface{}{
			"type": "api_key",
			"keys": []interface{}{"the-right-key"},
		},
	})

	if err := r.startWorkflowAPI(); err != nil {
		t.Fatalf("startWorkflowAPI: %v", err)
	}
	if r.workflowAPIServer == nil {
		t.Fatal("nothing was started")
	}

	if err := r.stopWorkflowAPI(context.Background()); err != nil {
		t.Errorf("stopWorkflowAPI: %v", err)
	}
	if r.workflowAPIServer != nil {
		t.Error("the server is still held after being stopped")
	}
	// Stopping twice is what shutdown does when something else got there
	// first, and it must not be an error.
	if err := r.stopWorkflowAPI(context.Background()); err != nil {
		t.Errorf("second stop: %v", err)
	}
}

func TestAPortAlreadyTakenIsReported(t *testing.T) {
	// Rather than a service that starts and serves nothing.
	held := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer held.Close()

	addr := strings.TrimPrefix(held.URL, "http://")
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("unexpected address %q", addr)
	}

	r := runtimeWithWorkflowAPI(t, &parser.WorkflowAPIConfig{
		Host: host,
		Port: atoi(t, port),
		Auth: map[string]interface{}{"type": "api_key", "keys": []interface{}{"k"}},
	})

	err := r.startWorkflowAPI()
	if err == nil {
		_ = r.stopWorkflowAPI(context.Background())
		t.Fatal("a port already in use was reported as started")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("the error does not say what failed: %v", err)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("not a port: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

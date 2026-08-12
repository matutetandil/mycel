package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MountHandler exists for paths that belong to the service rather than to a
// flow — the auth endpoints are the case it was added for. A connector that
// accepted them and never served them would look wired and change nothing.

func TestAMountedHandlerIsServed(t *testing.T) {
	c := New("api", 0, nil, nil)
	c.MountHandler("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	c.setupRoutes()

	rec := httptest.NewRecorder()
	c.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the mounted handler to have answered", rec.Code)
	}
}

func TestAFlowKeepsAPathAMountedHandlerAlsoWants(t *testing.T) {
	// Registering the same pattern twice panics the mux, so one of them has to
	// yield. A flow is written for this service specifically, so it wins — the
	// same reasoning as the built-in health endpoints.
	c := New("api", 0, nil, nil)
	c.RegisterRoute("POST /auth/login", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"from": "flow"}, nil
	})
	c.MountHandler("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	// The point is that this does not panic.
	c.setupRoutes()

	rec := httptest.NewRecorder()
	c.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	if rec.Code == http.StatusTeapot {
		t.Error("the mounted handler displaced the flow that claimed the path")
	}
}

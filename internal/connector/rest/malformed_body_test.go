package rest

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/sanitize"
)

// A body that does not parse is a bad request, not an empty one.
//
// The decode error was dropped, so a corrupt payload was indistinguishable
// from no payload: `POST /echo` with `{"broken":` answered 200 and echoed
// nothing, and a flow with a transform blamed a missing key in a 500. Either
// way the client was told something other than "your JSON is broken".
func TestMalformedBodyIsABadRequest(t *testing.T) {
	conn := New("test", 3000, nil, nil)

	called := false
	conn.RegisterRoute("POST /things", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		called = true
		return map[string]interface{}{"ok": true}, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("POST", "/things", strings.NewReader(`{"broken":`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 400 {
		t.Errorf("status = %d, want 400 (body: %s)", rr.Code, rr.Body.String())
	}
	if called {
		t.Error("the flow ran against a body that never parsed")
	}
}

// An empty body still is an empty body: a POST with nothing in it reaches the
// flow, as it always did.
func TestEmptyBodyStillReachesTheFlow(t *testing.T) {
	conn := New("test", 3000, nil, nil)

	called := false
	conn.RegisterRoute("POST /things", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		called = true
		return map[string]interface{}{"ok": true}, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("POST", "/things", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Error("the flow did not run for an empty body")
	}
}

// Input the sanitizer turned away is a bad request, not a server failure.
//
// It came back as a 500, which reads as Mycel breaking and — because 5xx is
// the retryable class — had clients and load balancers re-sending a payload
// that could never be accepted.
func TestSanitizerRejectionIsABadRequest(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.RegisterRoute("POST /things", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("input sanitization failed: %w", sanitize.ErrRejected)
	})
	conn.setupRoutes()

	req := httptest.NewRequest("POST", "/things", strings.NewReader(`{"ok":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 400 {
		t.Errorf("status = %d, want 400 (body: %s)", rr.Code, rr.Body.String())
	}
}

// A genuine failure inside the flow is still ours to own.
func TestFlowFailureIsStillAServerError(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.RegisterRoute("POST /things", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("connection refused")
	})
	conn.setupRoutes()

	req := httptest.NewRequest("POST", "/things", strings.NewReader(`{"ok":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 500 {
		t.Errorf("status = %d, want 500 (body: %s)", rr.Code, rr.Body.String())
	}
}

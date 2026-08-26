package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
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

package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestQueryMethod_BodyReachesInput verifies the RFC 10008 happy path: a QUERY
// request's body is decoded and merged into the flow input, alongside query
// string parameters.
func TestQueryMethod_BodyReachesInput(t *testing.T) {
	conn := New("test", 3000, nil, nil)

	var got map[string]interface{}
	conn.RegisterRoute("QUERY /search", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		got = input
		return map[string]interface{}{"ok": true}, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("QUERY", "/search?page=2", strings.NewReader(`{"name_like":"mat","limit":10}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if got == nil {
		t.Fatal("handler was not invoked")
	}
	if got["name_like"] != "mat" {
		t.Errorf("input[name_like] = %v, want \"mat\" (body not decoded)", got["name_like"])
	}
	if got["page"] != "2" {
		t.Errorf("input[page] = %v, want \"2\" (query params must still merge)", got["page"])
	}
}

// TestQueryMethod_MissingContentTypeRejected: RFC 10008 requires rejecting
// QUERY content without a declared media type with a 4xx.
func TestQueryMethod_MissingContentTypeRejected(t *testing.T) {
	conn := New("test", 3000, nil, nil)

	called := false
	conn.RegisterRoute("QUERY /search", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		called = true
		return nil, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("QUERY", "/search", strings.NewReader(`{"q":"x"}`))
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 415 {
		t.Errorf("status = %d, want 415", rr.Code)
	}
	if called {
		t.Error("handler must not run for QUERY without Content-Type")
	}
}

// TestQueryMethod_Unregistered405: QUERY on a path that only registered GET
// must fall through to the standard 405.
func TestQueryMethod_Unregistered405(t *testing.T) {
	conn := New("test", 3000, nil, nil)

	conn.RegisterRoute("GET /search", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("QUERY", "/search", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 405 {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

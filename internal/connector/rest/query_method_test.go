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
	if aq := rr.Header().Get("Accept-Query"); aq == "" {
		t.Error("Accept-Query header missing on a path with a QUERY handler")
	}
}

// TestQueryMethod_AcceptQueryOnSiblingMethods: the Accept-Query header
// advertises QUERY support for the path, so it must also appear on responses
// to other methods on that path — and never on paths without a QUERY handler.
func TestQueryMethod_AcceptQueryOnSiblingMethods(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.RegisterRoute("GET /search", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})
	conn.RegisterRoute("QUERY /search", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})
	conn.RegisterRoute("GET /plain", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("GET", "/search", nil)
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)
	if aq := rr.Header().Get("Accept-Query"); aq == "" {
		t.Error("Accept-Query missing on GET response for a QUERY-capable path")
	}

	req = httptest.NewRequest("GET", "/plain", nil)
	rr = httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)
	if aq := rr.Header().Get("Accept-Query"); aq != "" {
		t.Errorf("Accept-Query = %q on a path without QUERY support, want empty", aq)
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

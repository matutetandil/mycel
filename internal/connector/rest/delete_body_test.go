package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// A DELETE with a body is how a selective endpoint is shaped — "delete these
// ids", "drop these keys" — and RFC 9110 does not forbid it. The body was
// decoded for POST, PUT, PATCH and QUERY and never for DELETE, so such a flow
// received no fields at all: not an error, an empty input, which usually falls
// through to whatever the "no arguments" branch does. In the service that
// found this, that branch was "clear everything".

func deleteRoute(t *testing.T) (*Connector, *map[string]interface{}) {
	t.Helper()
	conn := New("test", 3000, nil, nil)
	var got map[string]interface{}
	conn.RegisterRoute("DELETE /keys/:scope", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		got = input
		return map[string]interface{}{"ok": true}, nil
	})
	conn.setupRoutes()
	return conn, &got
}

func TestADeleteBodyReachesTheFlow(t *testing.T) {
	conn, got := deleteRoute(t)

	req := httptest.NewRequest("DELETE", "/keys/products?dry_run=1", strings.NewReader(`{"ids":["a","b"],"reason":"stale"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	input := *got
	if input == nil {
		t.Fatal("handler was not invoked")
	}
	ids, ok := input["ids"].([]interface{})
	if !ok || len(ids) != 2 {
		t.Errorf("input[ids] = %#v, want the two ids from the body: the DELETE body was not decoded", input["ids"])
	}
	if input["reason"] != "stale" {
		t.Errorf("input[reason] = %v, want \"stale\"", input["reason"])
	}
	if input["scope"] != "products" {
		t.Errorf("input[scope] = %v, want the path parameter alongside the body", input["scope"])
	}
	if input["dry_run"] != "1" {
		t.Errorf("input[dry_run] = %v, want the query parameter alongside the body", input["dry_run"])
	}
}

func TestADeleteWithoutABodyIsStillADelete(t *testing.T) {
	// The common case, and it must not start failing because the rare one
	// works: no body means nothing to decode, and the flow sees what it saw.
	conn, got := deleteRoute(t)

	req := httptest.NewRequest("DELETE", "/keys/products", nil)
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	input := *got
	if input["scope"] != "products" {
		t.Errorf("input[scope] = %v, want the path parameter", input["scope"])
	}
	for k := range input {
		if k != "scope" && k != "headers" {
			t.Errorf("input carries %q, want nothing beyond the path parameter and headers: %v", k, input)
		}
	}
}

func TestACorruptDeleteBodyIsRefusedNotEmptied(t *testing.T) {
	// Same rule as every other body: a payload that does not parse is a bad
	// request, not an empty one — the silent-empty case is the one this
	// endpoint shape cannot afford.
	conn, got := deleteRoute(t)

	req := httptest.NewRequest("DELETE", "/keys/products", strings.NewReader(`{"ids": [`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 400 {
		t.Errorf("status = %d, want 400 for a body that does not parse (body: %s)", rr.Code, rr.Body.String())
	}
	if *got != nil {
		t.Errorf("the flow ran with input %v on a body that did not parse", *got)
	}
}

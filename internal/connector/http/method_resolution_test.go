package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

func TestResolveMethodPath(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		operation  string
		wantMethod string
		wantPath   string
	}{
		// split form — the contract production configs rely on; must not change
		{"split write", "/orders", "POST", "POST", "/orders"},
		{"split read", "/users", "GET", "GET", "/users"},
		{"split query", "/search", "QUERY", "QUERY", "/search"},
		// combined form in target + runtime DB-flavored default (the clobber bug)
		{"target method beats SELECT", "GET /users", "SELECT", "GET", "/users"},
		{"target method beats INSERT", "POST /orders", "INSERT", "POST", "/orders"},
		{"target method beats UPDATE", "PUT /orders/1", "UPDATE", "PUT", "/orders/1"},
		{"target QUERY beats SELECT", "QUERY /search", "SELECT", "QUERY", "/search"},
		// combined form in operation — the form the connector schema documents
		{"combined in operation", "", "POST /orders", "POST", "/orders"},
		{"combined in operation lowercase", "", "get /users", "GET", "/users"},
		{"combined in operation with target set", "/ignored", "PUT /orders/1", "PUT", "/orders/1"},
		// DB-flavored operation with no method in target → HTTP equivalent
		{"SELECT maps to GET", "/users", "SELECT", "GET", "/users"},
		{"INSERT maps to POST", "/orders", "INSERT", "POST", "/orders"},
		{"UPDATE maps to PUT", "/orders", "UPDATE", "PUT", "/orders"},
		// DELETE is both a verb and a DB op — always a valid method on the wire
		{"DELETE stays DELETE", "/orders/1", "DELETE", "DELETE", "/orders/1"},
		// unknown operations are ignored
		{"unknown op ignored", "GET /users", "CONSUME", "GET", "/users"},
		{"empty op", "GET /users", "", "GET", "/users"},
		{"bare path defaults GET", "/users", "", "GET", "/users"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method, path := resolveMethodPath(tc.target, tc.operation)
			if method != tc.wantMethod || path != tc.wantPath {
				t.Errorf("resolveMethodPath(%q, %q) = (%s, %s), want (%s, %s)",
					tc.target, tc.operation, method, path, tc.wantMethod, tc.wantPath)
			}
		})
	}
}

// captureServer records the last request's method, path+query, and body.
type capture struct {
	method string
	url    string
	body   string
}

func captureServer(c *capture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.method = r.Method
		c.url = r.URL.String()
		c.body = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

// TestWrite_RuntimeWriteFlowShape: what the runtime's handleCreate sends for
// to { target = "POST /orders" } — the INSERT default must not clobber the
// verb, and the payload must go out as the body.
func TestWrite_RuntimeWriteFlowShape(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Write(context.Background(), &connector.Data{
		Target:    "POST /orders",
		Operation: "INSERT",
		Payload:   map[string]interface{}{"sku": "X1"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.method != "POST" || got.url != "/orders" {
		t.Errorf("sent %s %s, want POST /orders", got.method, got.url)
	}
	if !strings.Contains(got.body, `"sku"`) {
		t.Errorf("body = %q, want the payload encoded", got.body)
	}
}

// TestWrite_CombinedFormInOperation: the schema-documented form
// to { operation = "POST /orders" }.
func TestWrite_CombinedFormInOperation(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Write(context.Background(), &connector.Data{
		Operation: "POST /orders",
		Payload:   map[string]interface{}{"sku": "X1"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.method != "POST" || got.url != "/orders" {
		t.Errorf("sent %s %s, want POST /orders", got.method, got.url)
	}
	if !strings.Contains(got.body, `"sku"`) {
		t.Errorf("body = %q, want the payload encoded", got.body)
	}
}

// TestWrite_SplitFormUnchanged: verb in operation, path in target — the form
// production aspects/flows use today. Must keep working identically.
func TestWrite_SplitFormUnchanged(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Write(context.Background(), &connector.Data{
		Target:    "/hook",
		Operation: "POST",
		Payload:   map[string]interface{}{"event": "done"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.method != "POST" || got.url != "/hook" {
		t.Errorf("sent %s %s, want POST /hook", got.method, got.url)
	}
	if !strings.Contains(got.body, `"event"`) {
		t.Errorf("body = %q, want the payload encoded", got.body)
	}
}

// TestRead_RuntimeReadFlowShape: what the runtime's handleRead sends for
// to { target = "GET /users" } — the SELECT default must not go on the wire.
func TestRead_RuntimeReadFlowShape(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Read(context.Background(), connector.Query{
		Target:    "GET /users",
		Operation: "SELECT",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.method != "GET" || got.url != "/users" {
		t.Errorf("sent %s %s, want GET /users", got.method, got.url)
	}
}

// TestRead_QueryMethodSendsFiltersAsBody: for QUERY (RFC 10008) the criteria
// travel in the body, not the query string.
func TestRead_QueryMethodSendsFiltersAsBody(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Read(context.Background(), connector.Query{
		Target:    "QUERY /search",
		Operation: "SELECT",
		Filters:   map[string]interface{}{"name_like": "%pro%"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.method != "QUERY" || got.url != "/search" {
		t.Errorf("sent %s %s, want QUERY /search (no query params)", got.method, got.url)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(got.body), &body); err != nil || body["name_like"] != "%pro%" {
		t.Errorf("body = %q, want filters encoded as JSON body", got.body)
	}
}

// TestCall_BodyVerbSendsParamsAsBody: saga/step actions like
// action { operation = "POST /reserve", body = {...} } dispatch through
// Call — params must travel as the encoded request body.
func TestCall_BodyVerbSendsParamsAsBody(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	result, err := c.Call(context.Background(), "POST /reserve", map[string]interface{}{
		"sku": "X1", "quantity": 2,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.method != "POST" || got.url != "/reserve" {
		t.Errorf("sent %s %s, want POST /reserve", got.method, got.url)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(got.body), &body); err != nil || body["sku"] != "X1" {
		t.Errorf("body = %q, want params encoded as JSON body", got.body)
	}
	// single-object response unwraps to a map, usable as step.<name>.<field>
	if m, ok := result.(map[string]interface{}); !ok || m["ok"] != true {
		t.Errorf("result = %#v, want unwrapped single object", result)
	}
}

// TestCall_GetSendsParamsAsQueryString: read verbs keep params on the URL.
func TestCall_GetSendsParamsAsQueryString(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Call(context.Background(), "GET /status", map[string]interface{}{"id": 7})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.method != "GET" || got.url != "/status?id=7" {
		t.Errorf("sent %s %s, want GET /status?id=7", got.method, got.url)
	}
	if got.body != "" {
		t.Errorf("body = %q, want empty for GET", got.body)
	}
}

// TestCall_BarePathDefaultsToGet: an operation without a verb is a GET.
func TestCall_BarePathDefaultsToGet(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Call(context.Background(), "/health", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.method != "GET" || got.url != "/health" {
		t.Errorf("sent %s %s, want GET /health", got.method, got.url)
	}
}

// TestRead_FiltersStillGoToQueryString: non-QUERY reads keep the existing
// behavior — filters as query string parameters.
func TestRead_FiltersStillGoToQueryString(t *testing.T) {
	var got capture
	srv := captureServer(&got)
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 1)
	_, err := c.Read(context.Background(), connector.Query{
		Target:  "/users",
		Filters: map[string]interface{}{"page": 2},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.method != "GET" || got.url != "/users?page=2" {
		t.Errorf("sent %s %s, want GET /users?page=2", got.method, got.url)
	}
	if got.body != "" {
		t.Errorf("body = %q, want empty for GET", got.body)
	}
}

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A step can address a REST API the way REST APIs are addressed.
//
// The path was concatenated as written and every parameter went to the query
// string or the body, so `GET /customers/42` could not be expressed at all.
// The documentation had invented `"GET /customers/${step.order.customer_id}"`
// for it in eight places — HCL interpolation of a CEL variable, which does not
// exist when the configuration is read, so the attribute could not be
// evaluated and the step ended up with no operation whatsoever.
func TestACallFillsPathParameters(t *testing.T) {
	var got struct {
		path  string
		query string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42","name":"Ada"}`))
	}))
	defer upstream.Close()

	c := New("api", upstream.URL, 5*time.Second, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	answer, err := c.Call(context.Background(), "GET /customers/:id",
		map[string]interface{}{"id": "42", "expand": "orders"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	if got.path != "/customers/42" {
		t.Errorf("path = %q, want the parameter in the path", got.path)
	}
	// The one that named a segment is spent there; the rest still travel.
	if got.query != "expand=orders" {
		t.Errorf("query = %q, want only what the path did not consume", got.query)
	}
	if row, ok := answer.(map[string]interface{}); !ok || row["name"] != "Ada" {
		t.Errorf("answer = %#v", answer)
	}
}

// The other spelling, and a segment nothing supplies.
func TestPathParameterSpellingsAndGaps(t *testing.T) {
	for _, c := range []struct {
		name     string
		path     string
		params   map[string]interface{}
		wantPath string
		wantLeft int
	}{
		{"colon", "/customers/:id", map[string]interface{}{"id": 7}, "/customers/7", 0},
		{"braces", "/customers/{id}", map[string]interface{}{"id": 7}, "/customers/7", 0},
		{"two of them", "/a/:x/b/:y", map[string]interface{}{"x": 1, "y": 2}, "/a/1/b/2", 0},
		{"nothing supplies it", "/customers/:id", map[string]interface{}{"other": 1}, "/customers/:id", 1},
		{"no parameters at all", "/customers", map[string]interface{}{"a": 1}, "/customers", 1},
		{"a value that needs escaping", "/files/:name", map[string]interface{}{"name": "a b"}, "/files/a%20b", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			path, left := fillPathParams(c.path, c.params)
			if path != c.wantPath {
				t.Errorf("path = %q, want %q", path, c.wantPath)
			}
			if len(left) != c.wantLeft {
				t.Errorf("%d parameters left, want %d", len(left), c.wantLeft)
			}
		})
	}
}

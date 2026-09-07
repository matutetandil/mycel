package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A request can carry headers of its own.
//
// The only headers that reached the wire were the connector's static
// `headers = {...}` plus auth. An upstream that selects a tenant, locale or
// store view by header — the same URL, the same query, a different `Store:`
// — could only be addressed with one connector per possible value, and one
// step per connector, each guarded with a `when`.
func TestARequestSendsTheHeadersItCarries(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	c := New("api", upstream.URL, 5*time.Second, nil, map[string]string{"Store": "default", "X-Static": "yes"}, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := connector.WithRequestHeaders(context.Background(), map[string]string{"Store": "au"})

	if _, err := c.Call(ctx, "GET /page", nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	// The request's header wins over the connector's; the rest still travel.
	if got := seen.Get("Store"); got != "au" {
		t.Errorf("Call: Store = %q, want the request's", got)
	}
	if got := seen.Get("X-Static"); got != "yes" {
		t.Errorf("Call: the connector's own header was lost: %q", got)
	}

	if _, err := c.Read(ctx, connector.Query{Target: "/page", Operation: "GET"}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := seen.Get("Store"); got != "au" {
		t.Errorf("Read: Store = %q", got)
	}

	if _, err := c.Write(ctx, &connector.Data{Target: "/page", Operation: "POST", Payload: map[string]interface{}{"a": 1}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := seen.Get("Store"); got != "au" {
		t.Errorf("Write: Store = %q", got)
	}

	// Without any, the connector's own are what is sent.
	if _, err := c.Call(context.Background(), "GET /page", nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := seen.Get("Store"); got != "default" {
		t.Errorf("a bare request sent Store = %q, want the connector's", got)
	}
}

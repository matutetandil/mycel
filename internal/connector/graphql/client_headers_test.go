package graphql

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A request to a GraphQL API can carry headers of its own, over the ones the
// connector sends on every request. A multi-store commerce backend reads the
// store view from a `Store:` header: same endpoint, same query, different
// currency, language and URLs.
func TestAGraphQLRequestSendsTheHeadersItCarries(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"page":{"title":"t"}}}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient("backend", &ClientConfig{
		Endpoint:   server.URL,
		RetryCount: 1,
		RetryDelay: time.Millisecond,
		Headers:    map[string]string{"Store": "default", "X-Static": "yes"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := connector.WithRequestHeaders(context.Background(), map[string]string{"Store": "au"})
	if _, err := client.Call(ctx, `{ page(id: 1) { title } }`, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := seen.Get("Store"); got != "au" {
		t.Errorf("Store = %q, want the request's", got)
	}
	if got := seen.Get("X-Static"); got != "yes" {
		t.Errorf("the connector's own header was lost: %q", got)
	}

	if _, err := client.Call(context.Background(), `{ page(id: 1) { title } }`, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := seen.Get("Store"); got != "default" {
		t.Errorf("a bare request sent Store = %q, want the connector's", got)
	}
}

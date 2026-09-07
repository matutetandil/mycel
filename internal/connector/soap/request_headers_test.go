package soap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A SOAP request can carry headers of its own, over the ones the connector
// sends on every request — the same rule the http and graphql clients follow,
// since a SOAP call is an HTTP request too.
func TestASOAPRequestSendsTheHeadersItCarries(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body><GetOrderResponse><id>1</id></GetOrderResponse></soap:Body>
</soap:Envelope>`))
	}))
	defer server.Close()

	client := NewClient("erp", server.URL, "1.1", "http://example.com/service", 5*time.Second, nil,
		map[string]string{"Store": "default", "X-Static": "yes"})

	ctx := connector.WithRequestHeaders(context.Background(), map[string]string{"Store": "au"})
	if _, err := client.Read(ctx, connector.Query{Operation: "GetOrder", Filters: map[string]interface{}{"id": "1"}}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := seen.Get("Store"); got != "au" {
		t.Errorf("Store = %q, want the request's", got)
	}
	if got := seen.Get("X-Static"); got != "yes" {
		t.Errorf("the connector's own header was lost: %q", got)
	}
	if got := seen.Get("SOAPAction"); got == "" {
		t.Error("the SOAPAction header was lost")
	}

	if _, err := client.Write(context.Background(), &connector.Data{Operation: "GetOrder", Payload: map[string]interface{}{"id": "1"}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := seen.Get("Store"); got != "default" {
		t.Errorf("a bare request sent Store = %q, want the connector's", got)
	}
}

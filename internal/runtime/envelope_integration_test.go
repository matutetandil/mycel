package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/transform"
)

// TestEnvelopeAppliedOnHTTPCreate is the end-to-end test the v1.19.5 release
// missed: it drives a real FlowHandler through handleCreate against a real
// HTTP destination and inspects the bytes that landed on the wire. The body
// must be wrapped under the envelope key — anything else means the wire
// payload is wrong even when parser-level tests pass.
func TestEnvelopeAppliedOnHTTPCreate(t *testing.T) {
	// Capture the body the connector posts.
	var (
		mu       sync.Mutex
		gotBody  []byte
		gotPath  string
		gotMethod string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = body
		gotPath = r.URL.Path
		gotMethod = r.Method
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Build the HTTP connector pointing at the test server.
	dest := httpconn.New("magento", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}

	// Transformer with the rules the flow declares.
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	cfg := &flow.Config{
		Name: "magento_create",
		From: &flow.FromConfig{
			Connector: "rabbit",
			ConnectorParams: map[string]interface{}{
				"target": "all.in.magento.q",
			},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{
				"style_number": "input.body.payload.styleNumber",
				"name":         "input.body.payload.styleName",
			},
		},
		To: &flow.ToConfig{
			Connector: "magento",
			Envelope:  "productData",
			Parallel:  true,
			ConnectorParams: map[string]interface{}{
				"target":    "/rest/V1/products",
				"operation": "POST",
			},
		},
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Simulate the input shape an MQ source delivers.
	input := map[string]interface{}{
		"body": map[string]interface{}{
			"payload": map[string]interface{}{
				"styleNumber": "AI02LT",
				"styleName":   "Axil",
			},
		},
	}

	// Call handleCreate directly — same path the runtime takes for
	// MQ source + POST destination.
	if _, err := h.handleCreate(context.Background(), input, dest); err != nil {
		t.Fatalf("handleCreate: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/rest/V1/products" {
		t.Errorf("expected /rest/V1/products, got %s", gotPath)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v\nraw=%s", err, gotBody)
	}

	// The fix the user is asking about: the body must be wrapped under
	// "productData", not flat.
	inner, ok := decoded["productData"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected envelope key 'productData' as map; got body=%s", gotBody)
	}
	if inner["style_number"] != "AI02LT" {
		t.Errorf("expected style_number=AI02LT inside envelope, got %v", inner["style_number"])
	}
	if inner["name"] != "Axil" {
		t.Errorf("expected name=Axil inside envelope, got %v", inner["name"])
	}
	if _, has := decoded["style_number"]; has {
		t.Errorf("style_number leaked to root level (envelope not applied), got body=%s", gotBody)
	}
}

// TestEnvelopeAbsentMeansFlatBody confirms the default behavior is unchanged
// when the attribute is not set.
func TestEnvelopeAbsentMeansFlatBody(t *testing.T) {
	var (
		mu      sync.Mutex
		gotBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	cfg := &flow.Config{
		Name: "no_envelope",
		From: &flow.FromConfig{
			Connector:       "rabbit",
			ConnectorParams: map[string]interface{}{"target": "q"},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{
				"x": "input.body.x",
			},
		},
		To: &flow.ToConfig{
			Connector: "api",
			Parallel:  true,
			ConnectorParams: map[string]interface{}{
				"target":    "/post",
				"operation": "POST",
			},
		},
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"x": "v"}}
	if _, err := h.handleCreate(context.Background(), input, dest); err != nil {
		t.Fatalf("handleCreate: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var decoded map[string]interface{}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body invalid JSON: %v\nraw=%s", err, gotBody)
	}
	if decoded["x"] != "v" {
		t.Errorf("expected flat body with x=v, got %s", gotBody)
	}
	if _, has := decoded["productData"]; has {
		t.Errorf("envelope key must not appear when not configured, got %s", gotBody)
	}
}

// keep imports tidy
var _ = bytes.NewReader

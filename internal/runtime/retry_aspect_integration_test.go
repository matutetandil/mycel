package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/internal/aspect"
	"github.com/matutetandil/mycel/internal/connector"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/transform"
)

// newRetryHandler builds a FlowHandler with HTTP destination + retry config.
// Returns the handler, a hit counter on the destination, and a cleanup
// function. The handler is used to assert retry semantics for various
// failure shapes.
func newRetryHandler(t *testing.T, status int, body string) (*FlowHandler, *atomic.Int32, func()) {
	t.Helper()
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}

	tr, _ := transform.NewCELTransformer()

	cfg := &flow.Config{
		Name: "retried",
		From: &flow.FromConfig{
			Connector:       "rabbit",
			ConnectorParams: map[string]interface{}{"target": "q"},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
		ErrorHandling: &flow.ErrorHandlingConfig{
			Retry: &flow.RetryConfig{
				Attempts: 3,
				Delay:    "1ms",
				Backoff:  "constant",
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

	return h, &hits, func() { srv.Close() }
}

// TestRetryStops_On4xxResponse: a 409 from Magento (or any 4xx) is a
// definitionally permanent failure — the same payload will always produce
// the same status. The retry budget should not consume more attempts.
func TestRetryStops_On4xxResponse(t *testing.T) {
	cases := []struct {
		name string
		code int
	}{
		{"409 Conflict", http.StatusConflict},
		{"400 Bad Request", http.StatusBadRequest},
		{"401 Unauthorized", http.StatusUnauthorized},
		{"404 Not Found", http.StatusNotFound},
		{"422 Unprocessable Entity", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, hits, done := newRetryHandler(t, tc.code, `{"err":"nope"}`)
			defer done()

			input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
			_, err := h.HandleRequest(context.Background(), input)
			if err == nil {
				t.Fatal("expected error from 4xx response")
			}
			if got := hits.Load(); got != 1 {
				t.Errorf("4xx must not be retried; expected 1 hit, got %d", got)
			}
		})
	}
}

// TestRetryContinues_On5xxResponse: 5xx is treated as transient — keep
// retrying through the full budget. (A flapping backend may recover.)
func TestRetryContinues_On5xxResponse(t *testing.T) {
	h, hits, done := newRetryHandler(t, http.StatusServiceUnavailable, `{"err":"down"}`)
	defer done()

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	_, err := h.HandleRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error from 5xx after exhausting retries")
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("5xx should consume the full retry budget; expected 3 hits, got %d", got)
	}
}

// recordingAspectAction is a fake aspect connector that records every text
// it receives — used to assert how many times an aspect fires per delivery.
type recordingAspectAction struct {
	mu       atomic.Int32
	messages []string
}

func (a *recordingAspectAction) increment(text string) {
	a.mu.Add(1)
	a.messages = append(a.messages, text)
}

// TestAfterAspectFiresOnceOnSuccess: a successful flow should fire the
// after aspect exactly once even when retry config is present. Pre-fix
// the layering ran retries OUTSIDE aspects, so a flow that succeeded on
// attempt 1 still emitted one after notification — but a flow that
// failed two attempts and succeeded on the third would emit three
// after notifications + two on_error. Post-fix: one each.
func TestAfterAspectFiresOnceOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var afterHits, errorHits atomic.Int32
	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())

	// Aspect connector is a faux Slack: each Write call increments the
	// matching counter based on the text payload.
	aspectConn := &countingWriter{
		afterHits: &afterHits,
		errorHits: &errorHits,
	}

	tr, _ := transform.NewCELTransformer()
	connectors := connector.NewRegistry()
	connectors.RegisterFactory(testConnectorFactory{name: "fake-slack", conn: aspectConn})
	if err := connectors.Register(context.Background(), &connector.Config{Name: "slack", Type: "fake-slack"}); err != nil {
		t.Fatalf("registry: %v", err)
	}

	asReg := aspect.NewRegistry()
	if err := asReg.RegisterAll([]*aspect.Config{
		{
			Name: "slack_notifier",
			On:   []string{"flow_x"},
			When: aspect.After,
			Action: &aspect.ActionConfig{
				Connector: "slack",
				Transform: map[string]string{"text": "'AFTER'"},
			},
		},
		{
			Name: "slack_error_notifier",
			On:   []string{"flow_x"},
			When: aspect.OnError,
			Action: &aspect.ActionConfig{
				Connector: "slack",
				Transform: map[string]string{"text": "'ERROR'"},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	exec, err := aspect.NewExecutor(asReg, connectors)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	cfg := &flow.Config{
		Name: "flow_x",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
		ErrorHandling: &flow.ErrorHandlingConfig{
			Retry: &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
		},
	}

	h := &FlowHandler{
		Config:         cfg,
		SourceType:     "mq",
		Dest:           dest,
		Transformer:    tr,
		AspectExecutor: exec,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if got := afterHits.Load(); got != 1 {
		t.Errorf("after aspect should fire once on success, got %d", got)
	}
	if got := errorHits.Load(); got != 0 {
		t.Errorf("on_error must not fire on success, got %d", got)
	}
}

// TestAfterDoesNotFireOnFailure: a failing flow (4xx, no retries because
// permanent) must NOT fire the after aspect. Only on_error.
func TestAfterDoesNotFireOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusConflict) // 409
		_, _ = w.Write([]byte(`{"err":"dup"}`))
	}))
	defer srv.Close()

	var afterHits, errorHits atomic.Int32

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())

	aspectConn := &countingWriter{afterHits: &afterHits, errorHits: &errorHits}
	tr, _ := transform.NewCELTransformer()
	connectors := connector.NewRegistry()
	connectors.RegisterFactory(testConnectorFactory{name: "fake-slack", conn: aspectConn})
	_ = connectors.Register(context.Background(), &connector.Config{Name: "slack", Type: "fake-slack"})

	asReg := aspect.NewRegistry()
	_ = asReg.RegisterAll([]*aspect.Config{
		{
			Name: "after_only", On: []string{"flow_x"}, When: aspect.After,
			Action: &aspect.ActionConfig{Connector: "slack", Transform: map[string]string{"text": "'AFTER'"}},
		},
		{
			Name: "on_error_only", On: []string{"flow_x"}, When: aspect.OnError,
			Action: &aspect.ActionConfig{Connector: "slack", Transform: map[string]string{"text": "'ERROR'"}},
		},
	})

	exec, _ := aspect.NewExecutor(asReg, connectors)

	cfg := &flow.Config{
		Name: "flow_x",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
		ErrorHandling: &flow.ErrorHandlingConfig{
			Retry: &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
		},
	}

	h := &FlowHandler{
		Config:         cfg,
		SourceType:     "mq",
		Dest:           dest,
		Transformer:    tr,
		AspectExecutor: exec,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	_, _ = h.HandleRequest(context.Background(), input)

	if got := afterHits.Load(); got != 0 {
		t.Errorf("after must NOT fire on failure, got %d", got)
	}
	if got := errorHits.Load(); got != 1 {
		t.Errorf("on_error should fire exactly once per delivery (not per attempt), got %d", got)
	}
}

// TestErrorVariableIsMapWithMessageField: ensures `error.message` resolves
// against an HTTP error in an on_error aspect — the bug where `error` was
// declared as cel.StringType made every reference to error.message fail
// with "type 'string' does not support field selection".
func TestErrorVariableIsMapWithMessageField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`already exists`))
	}))
	defer srv.Close()

	var captured string
	var captureCount atomic.Int32
	aspectConn := &capturingWriter{captured: &captured, count: &captureCount}

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	tr, _ := transform.NewCELTransformer()

	connectors := connector.NewRegistry()
	connectors.RegisterFactory(testConnectorFactory{name: "fake-slack", conn: aspectConn})
	_ = connectors.Register(context.Background(), &connector.Config{Name: "slack", Type: "fake-slack"})

	asReg := aspect.NewRegistry()
	_ = asReg.RegisterAll([]*aspect.Config{
		{
			Name: "alert", On: []string{"flow_x"}, When: aspect.OnError,
			Action: &aspect.ActionConfig{
				Connector: "slack",
				Transform: map[string]string{
					"text": "'failed: ' + error.message + ' (code ' + string(error.code) + ', type=' + error.type + ')'",
				},
			},
		},
	})
	exec, _ := aspect.NewExecutor(asReg, connectors)

	cfg := &flow.Config{
		Name: "flow_x",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
	}

	h := &FlowHandler{
		Config:         cfg,
		SourceType:     "mq",
		Dest:           dest,
		Transformer:    tr,
		AspectExecutor: exec,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	_, _ = h.HandleRequest(context.Background(), input)

	if captureCount.Load() != 1 {
		t.Fatalf("expected on_error aspect to fire once, got %d", captureCount.Load())
	}
	if !strings.Contains(captured, "failed:") {
		t.Errorf("expected 'failed:' in message, got %q", captured)
	}
	if !strings.Contains(captured, "code 409") {
		t.Errorf("expected 'code 409' in message (error.code resolved to int), got %q", captured)
	}
	if !strings.Contains(captured, "type=http") {
		t.Errorf("expected 'type=http' in message, got %q", captured)
	}
}

// --- test helpers -----------------------------------------------------------

type testConnectorFactory struct {
	name string
	conn connector.Connector
}

func (f testConnectorFactory) Type() string                       { return f.name }
func (f testConnectorFactory) Supports(t, _ string) bool          { return t == f.name }
func (f testConnectorFactory) Create(_ context.Context, _ *connector.Config) (connector.Connector, error) {
	return f.conn, nil
}

type countingWriter struct {
	afterHits *atomic.Int32
	errorHits *atomic.Int32
}

func (w *countingWriter) Name() string                           { return "slack" }
func (w *countingWriter) Type() string                           { return "fake-slack" }
func (w *countingWriter) Connect(_ context.Context) error        { return nil }
func (w *countingWriter) Close(_ context.Context) error          { return nil }
func (w *countingWriter) Health(_ context.Context) error         { return nil }
func (w *countingWriter) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	if data.Payload == nil {
		return &connector.Result{}, nil
	}
	if text, ok := data.Payload["text"].(string); ok {
		if strings.Contains(text, "AFTER") {
			w.afterHits.Add(1)
		}
		if strings.Contains(text, "ERROR") {
			w.errorHits.Add(1)
		}
	}
	return &connector.Result{}, nil
}

type capturingWriter struct {
	captured *string
	count    *atomic.Int32
}

func (w *capturingWriter) Name() string                    { return "slack" }
func (w *capturingWriter) Type() string                    { return "fake-slack" }
func (w *capturingWriter) Connect(_ context.Context) error { return nil }
func (w *capturingWriter) Close(_ context.Context) error   { return nil }
func (w *capturingWriter) Health(_ context.Context) error  { return nil }
func (w *capturingWriter) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	w.count.Add(1)
	if data.Payload != nil {
		if text, ok := data.Payload["text"].(string); ok {
			*w.captured = text
		}
	}
	return &connector.Result{}, nil
}

package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/internal/aspect"
	"github.com/matutetandil/mycel/internal/connector"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// dropCapturingWriter records every drop notification it sees so tests
// can assert which reason / policy fired.
type dropCapturingWriter struct {
	mu       atomic.Int32
	captured []string
}

func (w *dropCapturingWriter) Name() string                    { return "alerter" }
func (w *dropCapturingWriter) Type() string                    { return "fake-alerter" }
func (w *dropCapturingWriter) Connect(_ context.Context) error { return nil }
func (w *dropCapturingWriter) Close(_ context.Context) error   { return nil }
func (w *dropCapturingWriter) Health(_ context.Context) error  { return nil }
func (w *dropCapturingWriter) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	w.mu.Add(1)
	if data.Payload != nil {
		if text, ok := data.Payload["text"].(string); ok {
			w.captured = append(w.captured, text)
		}
	}
	return &connector.Result{}, nil
}

// TestOnDropAspect_FiresOnCoordinateTimeout: an aspect with `when =
// "on_drop"` must fire when coordinate.on_timeout="ack" deflects the
// message, and the `drop.reason` / `drop.policy` CEL bindings must
// resolve to "coordinate_timeout" / "ack".
func TestOnDropAspect_FiresOnCoordinateTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())

	alerter := &dropCapturingWriter{}
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	connectors := connector.NewRegistry()
	connectors.RegisterFactory(testConnectorFactory{name: "fake-alerter", conn: alerter})
	_ = connectors.Register(context.Background(), &connector.Config{Name: "alerter", Type: "fake-alerter"})

	asReg := aspect.NewRegistry()
	if err := asReg.RegisterAll([]*aspect.Config{
		{
			Name: "orphan_alert", On: []string{"orphan_flow"}, When: aspect.OnDrop,
			Action: &aspect.ActionConfig{
				Connector: "alerter",
				Transform: map[string]string{
					"text": "'orphaned: ' + drop.reason + ' policy=' + drop.policy",
				},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	exec, _ := aspect.NewExecutor(asReg, connectors)

	cfg := &flow.Config{
		Name: "orphan_flow",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Coordinate: &flow.CoordinateConfig{
			Storage:   &flow.SyncStorageConfig{Driver: "memory"},
			Timeout:   "100ms",
			OnTimeout: "ack",
			Wait: &flow.WaitConfig{
				When: "true",
				For:  "'never_emitted'",
			},
		},
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
		SyncManager:    mgr,
		AspectExecutor: exec,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "ORPHAN"}}
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if got := alerter.mu.Load(); got != 1 {
		t.Fatalf("on_drop aspect should fire exactly once, got %d", got)
	}
	if len(alerter.captured) != 1 {
		t.Fatalf("expected 1 captured message, got %d", len(alerter.captured))
	}
	want := "orphaned: coordinate_timeout policy=ack"
	if alerter.captured[0] != want {
		t.Errorf("expected %q, got %q", want, alerter.captured[0])
	}
}

// TestOnDropAspect_FiresOnFilterReject: filter-rejected messages also
// fire on_drop with reason="filter".
func TestOnDropAspect_FiresOnFilterReject(t *testing.T) {
	alerter := &dropCapturingWriter{}
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	connectors := connector.NewRegistry()
	connectors.RegisterFactory(testConnectorFactory{name: "fake-alerter", conn: alerter})
	_ = connectors.Register(context.Background(), &connector.Config{Name: "alerter", Type: "fake-alerter"})

	asReg := aspect.NewRegistry()
	_ = asReg.RegisterAll([]*aspect.Config{
		{
			Name: "drop_alert", On: []string{"filtered_flow"}, When: aspect.OnDrop,
			Action: &aspect.ActionConfig{
				Connector: "alerter",
				Transform: map[string]string{"text": "'reason=' + drop.reason"},
			},
		},
	})
	exec, _ := aspect.NewExecutor(asReg, connectors)

	cfg := &flow.Config{
		Name: "filtered_flow",
		From: &flow.FromConfig{
			Connector:       "rabbit",
			ConnectorParams: map[string]interface{}{"target": "q"},
			FilterConfig: &flow.FilterConfig{
				Condition: "input.body.kind == 'wanted'",
				OnReject:  "ack",
			},
		},
	}
	h := &FlowHandler{
		Config:         cfg,
		SourceType:     "mq",
		Transformer:    tr,
		SyncManager:    mgr,
		AspectExecutor: exec,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"kind": "unwanted"}}
	result, err := h.HandleRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	// Filter rejection defers on_drop firing via PendingOnDrop closure
	// so that fan-out aggregation can suppress losers. Real consumers
	// invoke flow.FireDropAspect after the handler returns; the test
	// mirrors that contract.
	flow.FireDropAspect(context.Background(), result)

	if got := alerter.mu.Load(); got != 1 {
		t.Fatalf("on_drop must fire on filter rejection, got %d", got)
	}
	if alerter.captured[0] != "reason=filter" {
		t.Errorf("expected reason=filter, got %q", alerter.captured[0])
	}
}

// TestOnDropAspect_DoesNotFireOnSuccess: regression — on_drop must only
// fire when the flow body was deflected, not on successful completion.
func TestOnDropAspect_DoesNotFireOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	alerter := &dropCapturingWriter{}
	tr, _ := transform.NewCELTransformer()

	connectors := connector.NewRegistry()
	connectors.RegisterFactory(testConnectorFactory{name: "fake-alerter", conn: alerter})
	_ = connectors.Register(context.Background(), &connector.Config{Name: "alerter", Type: "fake-alerter"})

	asReg := aspect.NewRegistry()
	_ = asReg.RegisterAll([]*aspect.Config{
		{
			Name: "drop_alert", On: []string{"happy_flow"}, When: aspect.OnDrop,
			Action: &aspect.ActionConfig{
				Connector: "alerter",
				Transform: map[string]string{"text": "'should_not_fire'"},
			},
		},
	})
	exec, _ := aspect.NewExecutor(asReg, connectors)

	cfg := &flow.Config{
		Name: "happy_flow",
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
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if got := alerter.mu.Load(); got != 0 {
		t.Errorf("on_drop must not fire on success, got %d invocations", got)
	}
}

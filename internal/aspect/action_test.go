package aspect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// What an aspect does when it fires: hand a record to a connector.
//
// It is how an audit trail is written and how a notification is sent, and it
// was covered as far as its arguments. The default matters as much as the
// rest: an action that names no operation writes, because the thing an aspect
// most often does is record that something happened.
type recordingWriter struct {
	name    string
	written []*connector.Data
	err     error
}

func (w *recordingWriter) Name() string                      { return w.name }
func (w *recordingWriter) Type() string                      { return "database" }
func (w *recordingWriter) Connect(ctx context.Context) error { return nil }
func (w *recordingWriter) Close(ctx context.Context) error   { return nil }
func (w *recordingWriter) Health(ctx context.Context) error  { return nil }

func (w *recordingWriter) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	if w.err != nil {
		return nil, w.err
	}
	w.written = append(w.written, data)
	return &connector.Result{Affected: 1}, nil
}

// readOnly can be read and not written to, like a CDC source.
type readOnly struct{ name string }

func (r *readOnly) Name() string                      { return r.name }
func (r *readOnly) Type() string                      { return "cdc" }
func (r *readOnly) Connect(ctx context.Context) error { return nil }
func (r *readOnly) Close(ctx context.Context) error   { return nil }
func (r *readOnly) Health(ctx context.Context) error  { return nil }

func executorWithConnectors(t *testing.T, conns map[string]connector.Connector) *Executor {
	t.Helper()
	registry := connector.NewRegistry()
	for name, conn := range conns {
		registry.Replace(name, conn)
	}
	e, err := NewExecutor(NewRegistry(), registry)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return e
}

func TestAnActionHandsTheRecordToItsConnector(t *testing.T) {
	audit := &recordingWriter{name: "audit_db"}
	e := executorWithConnectors(t, map[string]connector.Connector{"audit_db": audit})

	record := map[string]interface{}{"flow": "create_order", "at": "2026-08-21T00:00:00Z"}
	if err := e.executeConnectorAction(context.Background(), &ActionConfig{
		Connector: "audit_db",
		Target:    "audit_log",
	}, record); err != nil {
		t.Fatalf("action: %v", err)
	}

	if len(audit.written) != 1 {
		t.Fatalf("%d writes, want one", len(audit.written))
	}
	got := audit.written[0]
	if got.Target != "audit_log" {
		t.Errorf("target = %q", got.Target)
	}
	// An action that names no operation writes — recording that something
	// happened is what an aspect is usually for.
	if got.Operation != "INSERT" {
		t.Errorf("operation = %q, want the default", got.Operation)
	}
	if got.Payload["flow"] != "create_order" {
		t.Errorf("payload = %#v", got.Payload)
	}

	// And one that names an operation gets that one.
	if err := e.executeConnectorAction(context.Background(), &ActionConfig{
		Connector: "audit_db",
		Target:    "audit_log",
		Operation: "PUBLISH",
	}, record); err != nil {
		t.Fatalf("action: %v", err)
	}
	if audit.written[1].Operation != "PUBLISH" {
		t.Errorf("operation = %q", audit.written[1].Operation)
	}
}

func TestAnActionSaysWhyItCouldNotRun(t *testing.T) {
	e := executorWithConnectors(t, map[string]connector.Connector{
		"cdc_source": &readOnly{name: "cdc_source"},
		"audit_db":   &recordingWriter{name: "audit_db", err: errors.New("disk is full")},
	})
	ctx := context.Background()
	record := map[string]interface{}{"a": 1}

	err := e.executeConnectorAction(ctx, &ActionConfig{Connector: "nowhere"}, record)
	if err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("a connector that is not registered gave %v", err)
	}

	err = e.executeConnectorAction(ctx, &ActionConfig{Connector: "cdc_source"}, record)
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Errorf("a connector that cannot be written to gave %v", err)
	}

	err = e.executeConnectorAction(ctx, &ActionConfig{Connector: "audit_db"}, record)
	if err == nil || !strings.Contains(err.Error(), "disk is full") {
		t.Errorf("a write that failed gave %v, want what the connector said", err)
	}
}

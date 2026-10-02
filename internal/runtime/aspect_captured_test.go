package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
)

// What a transaction captures has to reach the aspects that run after it.
//
// The case that asked for it: a consumer writes an item in a transaction, and
// one statement compares the stored value of a flag with the incoming one.
// When the flag changed, other services must evict their caches — an HTTP call
// that only an `after` aspect can make safely (it runs once the write
// committed, and its failure does not touch the message's disposition). The
// call is expensive, so it must run only when the flag changed. The answer was
// computed, returned by the transaction, and dropped on its way to the
// aspect, which could then only run on every message or on none.

// sinkWriter records what an aspect action hands it.
type sinkWriter struct {
	mu      sync.Mutex
	written []map[string]interface{}
}

func (s *sinkWriter) Name() string                      { return "downstream" }
func (s *sinkWriter) Type() string                      { return "http" }
func (s *sinkWriter) Connect(ctx context.Context) error { return nil }
func (s *sinkWriter) Close(ctx context.Context) error   { return nil }
func (s *sinkWriter) Health(ctx context.Context) error  { return nil }

func (s *sinkWriter) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, data.Payload)
	return &connector.Result{Affected: 1}, nil
}

func (s *sinkWriter) calls() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]interface{}(nil), s.written...)
}

// flagChangeHandler is a transaction that captures whether the incoming flag
// differs from the stored one and then stores it, plus an `after` aspect that
// fires only when it did.
func flagChangeHandler(t *testing.T) (*FlowHandler, *sinkWriter) {
	t.Helper()

	h, db := newTxHandler(t, nil)
	if _, err := db.Exec(`INSERT INTO lookup (option_id, code, label) VALUES (1, 'SKU-1', 'off')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h.Config.To.Transaction = &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`SELECT (COALESCE((SELECT label FROM lookup WHERE code = :sku), '') <> :incoming) AS flag_changed`,
			"flag_changed", "", map[string]string{"sku": "input.sku", "incoming": "input.flag"}),
		mkExec(`UPDATE lookup SET label = :incoming WHERE code = :sku`,
			"", "", map[string]string{"sku": "input.sku", "incoming": "input.flag"}),
	}}

	sink := &sinkWriter{}
	h.Connectors.Replace("downstream", sink)

	aspects := aspect.NewRegistry()
	if err := aspects.Register(&aspect.Config{
		Name: "flag_change_invalidate",
		On:   []string{"tx_*"},
		When: aspect.After,
		If:   "has(result.captured.flag_changed) && result.captured.flag_changed == 1",
		Action: &aspect.ActionConfig{
			Connector: "downstream",
			Operation: "POST /cache/invalidate-flag",
			Transform: map[string]string{
				"sku":     "input.sku",
				"changed": "result.captured.flag_changed",
			},
		},
	}); err != nil {
		t.Fatalf("register aspect: %v", err)
	}
	executor, err := aspect.NewExecutor(aspects, h.Connectors)
	if err != nil {
		t.Fatalf("aspect executor: %v", err)
	}
	h.AspectExecutor = executor
	return h, sink
}

func TestAnAfterAspectSeesWhatATransactionCaptured(t *testing.T) {
	h, sink := flagChangeHandler(t)
	ctx := context.Background()

	// The flag changes: the aspect fires, and its action sees the value too.
	if _, err := h.HandleRequest(ctx, map[string]interface{}{"sku": "SKU-1", "flag": "on"}); err != nil {
		t.Fatalf("first message: %v", err)
	}
	calls := sink.calls()
	if len(calls) != 1 {
		t.Fatalf("the aspect ran %d times after the flag changed, want once", len(calls))
	}
	if calls[0]["sku"] != "SKU-1" {
		t.Errorf("action payload sku = %v", calls[0]["sku"])
	}
	if calls[0]["changed"] != int64(1) {
		t.Errorf("action payload changed = %#v, want the captured 1", calls[0]["changed"])
	}

	// Same flag again: nothing changed, so the expensive call is not made.
	if _, err := h.HandleRequest(ctx, map[string]interface{}{"sku": "SKU-1", "flag": "on"}); err != nil {
		t.Fatalf("second message: %v", err)
	}
	if got := len(sink.calls()); got != 1 {
		t.Errorf("the aspect ran on a message that changed nothing: %d calls", got)
	}
}

// A client of the flow keeps the captured values in the response whether or
// not an aspect is configured. Adding any aspect used to route the answer
// through a conversion that kept only the row count.
func TestTheResponseKeepsWhatATransactionCapturedWhenAspectsAreConfigured(t *testing.T) {
	h, _ := flagChangeHandler(t)

	got, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "SKU-1", "flag": "on"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	answer, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("answer = %#v", got)
	}
	captured, ok := answer["captured"].(map[string]interface{})
	if !ok {
		t.Fatalf("the answer lost what the transaction captured: %#v", answer)
	}
	if captured["flag_changed"] != int64(1) {
		t.Errorf("captured.flag_changed = %#v", captured["flag_changed"])
	}
}

func TestResultToConnectorResultCarriesTheCapturedValues(t *testing.T) {
	h := &FlowHandler{}

	res := h.resultToConnectorResult(map[string]interface{}{
		"affected": int64(2),
		"captured": map[string]interface{}{"product_id": int64(7)},
	})
	captured, ok := res.Metadata["captured"].(map[string]interface{})
	if !ok || captured["product_id"] != int64(7) {
		t.Fatalf("metadata = %#v, want the captured values", res.Metadata)
	}
	if res.Affected != 2 {
		t.Errorf("affected = %d", res.Affected)
	}

	// A write that captured nothing carries no metadata, as before.
	if res := h.resultToConnectorResult(map[string]interface{}{"affected": int64(1)}); res.Metadata != nil {
		t.Errorf("metadata = %#v, want none", res.Metadata)
	}
}

// A write flow keeps its answer once an aspect is configured, whatever kind of
// source triggered it. The aspect path read the intent from the source
// operation alone, so a source that does not speak in HTTP methods — here a
// method name, as gRPC, SOAP or TCP would give — was taken for a read and the
// write answered null.
func TestAWriteFromASourceWithoutHTTPMethodsKeepsItsAnswerUnderAspects(t *testing.T) {
	h, db := newTxHandler(t, nil)
	h.Config.From.ConnectorParams = map[string]interface{}{"operation": "SaveParent"}
	h.Config.To = &flow.ToConfig{
		Connector:       "db",
		ConnectorParams: map[string]interface{}{"target": "parent", "operation": "INSERT"},
	}

	aspects := aspect.NewRegistry()
	if err := aspects.Register(&aspect.Config{
		Name: "never", On: []string{"tx_*"}, When: aspect.After, If: "false",
		Action: &aspect.ActionConfig{Connector: "db"},
	}); err != nil {
		t.Fatalf("register aspect: %v", err)
	}
	executor, err := aspect.NewExecutor(aspects, h.Connectors)
	if err != nil {
		t.Fatalf("aspect executor: %v", err)
	}
	h.AspectExecutor = executor

	got, err := h.HandleRequest(context.Background(), map[string]interface{}{"name": "p"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if count(t, db, "parent") != 1 {
		t.Fatalf("the row was not written")
	}
	answer, ok := got.(map[string]interface{})
	if !ok || answer["affected"] != int64(1) {
		t.Errorf("answer = %#v, want the write's row count", got)
	}
}

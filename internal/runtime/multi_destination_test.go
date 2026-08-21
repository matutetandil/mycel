package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A flow with several destinations is how one message reaches the database, the
// queue and the audit log at once. What it gets wrong is not visible from the
// caller's side: the write that did not happen looks exactly like the write
// that did, because the flow answers on the strength of the others.

// recordingWriter keeps what it was asked to write.
type recordingWriter struct {
	name string
	err  error

	mu      sync.Mutex
	written []*connector.Data
}

func (w *recordingWriter) Name() string                  { return w.name }
func (w *recordingWriter) Type() string                  { return "fake" }
func (w *recordingWriter) Connect(context.Context) error { return nil }
func (w *recordingWriter) Close(context.Context) error   { return nil }
func (w *recordingWriter) Health(context.Context) error  { return nil }

func (w *recordingWriter) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written = append(w.written, data)
	if w.err != nil {
		return nil, w.err
	}
	return &connector.Result{Affected: 1}, nil
}

func (w *recordingWriter) writes() []*connector.Data {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*connector.Data(nil), w.written...)
}

func multiDestHandler(t *testing.T, destinations []*flow.ToConfig, writers map[string]*recordingWriter) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	for name, writer := range writers {
		registry.Replace(name, writer)
	}
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return &FlowHandler{
		Config: &flow.Config{
			Name:    "distribute_order",
			MultiTo: destinations,
		},
		Connectors:  registry,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestEveryDestinationReceivesTheMessage(t *testing.T) {
	db := &recordingWriter{name: "db"}
	queue := &recordingWriter{name: "queue"}
	audit := &recordingWriter{name: "audit"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "queue", Parallel: true, ConnectorParams: map[string]interface{}{"target": "orders.new"}},
		{Connector: "audit", ConnectorParams: map[string]interface{}{"target": "writes"}},
	}, map[string]*recordingWriter{"db": db, "queue": queue, "audit": audit})

	_, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1", "total": 42}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	for name, writer := range map[string]*recordingWriter{"db": db, "queue": queue, "audit": audit} {
		if len(writer.writes()) != 1 {
			t.Errorf("%s received %d writes, want one", name, len(writer.writes()))
		}
	}
}

func TestTheSameConnectorTwiceIsWrittenTwice(t *testing.T) {
	// Two tables in one database is the ordinary shape of this: an order and
	// its lines. Both writes have to happen even though the connector is the
	// same, and both have to be reported.
	db := &recordingWriter{name: "db"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "order_items"}},
	}, map[string]*recordingWriter{"db": db})

	result, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1"}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	writes := db.writes()
	if len(writes) != 2 {
		t.Fatalf("the connector received %d writes, want both", len(writes))
	}

	targets := map[string]bool{}
	for _, write := range writes {
		targets[write.Target] = true
	}
	if !targets["orders"] || !targets["order_items"] {
		t.Errorf("targets written = %v, want both tables", targets)
	}

	// And the report accounts for both, rather than one overwriting the other
	// because they share a connector name.
	report, ok := result.(*MultiDestResult)
	if !ok {
		t.Fatalf("result = %T", result)
	}
	if len(report.Results) != 2 {
		t.Errorf("the report covers %d writes, want both: %v", len(report.Results), report.Results)
	}
}

func TestADestinationCanShapeThePayloadItsOwnWay(t *testing.T) {
	// The reason several destinations are not one: each system wants the
	// record in its own shape.
	db := &recordingWriter{name: "db"}
	queue := &recordingWriter{name: "queue"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{
			Connector: "queue",
			Transform: map[string]string{
				"event":    `"order.created"`,
				"order_id": "input.id",
			},
			TransformOrder:  []string{"event", "order_id"},
			ConnectorParams: map[string]interface{}{"target": "orders.new"},
		},
	}, map[string]*recordingWriter{"db": db, "queue": queue})

	_, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1", "total": 42}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	sent := queue.writes()[0].Payload
	if sent["event"] != "order.created" || sent["order_id"] != "order-1" {
		t.Errorf("the queue was sent %v, want its own shape", sent)
	}

	// And the destination without a transform still gets the whole record.
	stored := db.writes()[0].Payload
	if stored["total"] == nil {
		t.Errorf("the database was sent %v, want the record as it was", stored)
	}
}

func TestADestinationCanBeSkippedByItsCondition(t *testing.T) {
	// One system wants only the orders above a threshold; the rest want all of
	// them.
	db := &recordingWriter{name: "db"}
	alerts := &recordingWriter{name: "alerts"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "alerts", When: "input.total > 1000", ConnectorParams: map[string]interface{}{"target": "big"}},
	}, map[string]*recordingWriter{"db": db, "alerts": alerts})

	_, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1", "total": 42}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	if len(db.writes()) != 1 {
		t.Error("the unconditional destination was skipped")
	}
	if len(alerts.writes()) != 0 {
		t.Error("a destination whose condition was false was written to anyway")
	}

	// And the same flow with a bigger order reaches both.
	_, err = h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-2", "total": 5000}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}
	if len(alerts.writes()) != 1 {
		t.Error("a destination whose condition was true was skipped")
	}
}

func TestOneDestinationFailingDoesNotStopTheOthers(t *testing.T) {
	// The point of writing to several: one being down should not cost the
	// others their copy.
	db := &recordingWriter{name: "db"}
	broken := &recordingWriter{name: "queue", err: errors.New("connection refused")}
	audit := &recordingWriter{name: "audit"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "queue", ConnectorParams: map[string]interface{}{"target": "orders.new"}},
		{Connector: "audit", ConnectorParams: map[string]interface{}{"target": "writes"}},
	}, map[string]*recordingWriter{"db": db, "queue": broken, "audit": audit})

	result, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1"}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	if len(db.writes()) != 1 || len(audit.writes()) != 1 {
		t.Error("a destination that was up missed its write because another was down")
	}

	// The failure is reported, and named, or nobody can act on it.
	report, ok := result.(*MultiDestResult)
	if !ok {
		t.Fatalf("result = %T", result)
	}
	if report.Success {
		t.Error("a write with a failed destination reported itself wholly successful")
	}
	if _, named := report.Errors["queue"]; !named {
		t.Errorf("errors = %v, want the destination that failed named", report.Errors)
	}
}

func TestWhenEveryDestinationFailsTheFlowFails(t *testing.T) {
	// Nothing was written anywhere, so the message must not be treated as
	// handled — for a queue source that is the difference between a retry and
	// a silent loss.
	first := &recordingWriter{name: "db", err: errors.New("connection refused")}
	second := &recordingWriter{name: "queue", err: errors.New("no route to host")}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "queue", ConnectorParams: map[string]interface{}{"target": "orders.new"}},
	}, map[string]*recordingWriter{"db": first, "queue": second})

	_, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1"}, Operation{Method: "INSERT"})
	if err == nil {
		t.Fatal("a write that reached nowhere reported success")
	}
	for _, want := range []string{"db", "queue"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestADestinationThatCannotBeWrittenToIsNamed(t *testing.T) {
	db := &recordingWriter{name: "db"}
	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "absent", ConnectorParams: map[string]interface{}{"target": "x"}},
	}, map[string]*recordingWriter{"db": db})

	result, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1"}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	report, _ := result.(*MultiDestResult)
	if report == nil || len(report.Errors) == 0 {
		t.Fatalf("a destination that does not exist was not reported: %v", result)
	}
	joined := strings.Join(valuesOf(report.Errors), " ")
	if !strings.Contains(joined, "absent") {
		t.Errorf("errors = %v, want the connector named", report.Errors)
	}
}

func TestParallelDestinationsAllArrive(t *testing.T) {
	// They run at once and report into one place, so the report has to survive
	// the concurrency.
	writers := map[string]*recordingWriter{}
	var destinations []*flow.ToConfig
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		writers[name] = &recordingWriter{name: name}
		destinations = append(destinations, &flow.ToConfig{
			Connector: name, Parallel: true,
			ConnectorParams: map[string]interface{}{"target": "t"},
		})
	}

	h := multiDestHandler(t, destinations, writers)
	result, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1"}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	for name, writer := range writers {
		if len(writer.writes()) != 1 {
			t.Errorf("%s received %d writes", name, len(writer.writes()))
		}
	}
	report, _ := result.(*MultiDestResult)
	if report == nil || len(report.Results) != len(writers) {
		t.Errorf("the report covers %d of %d writes", len(report.Results), len(writers))
	}
}

func TestAFlowWithNoDestinationsIsRefused(t *testing.T) {
	h := multiDestHandler(t, nil, nil)
	if _, err := h.handleMultiDestWrite(context.Background(), map[string]interface{}{}, Operation{}); err == nil {
		t.Error("a multi-destination write with no destinations reported success")
	}
}

func valuesOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestADestinationConditionReadsTheMessageTheWayEveryOtherOneDoes(t *testing.T) {
	// Every condition in a flow — a filter, an accept gate, a step, an aspect —
	// is written against `input`. A destination's was the exception: the fields
	// were copied to the top level, where CEL cannot see them, so the spelling
	// somebody would reach for first failed the write with "no such key"
	// instead of deciding it.
	for name, condition := range map[string]string{
		"the message as it arrived": "input.total > 1000",
		"what the transform made":   "output.total > 1000",
	} {
		t.Run(name, func(t *testing.T) {
			alerts := &recordingWriter{name: "alerts"}
			h := multiDestHandler(t, []*flow.ToConfig{
				{Connector: "alerts", When: condition, ConnectorParams: map[string]interface{}{"target": "big"}},
			}, map[string]*recordingWriter{"alerts": alerts})

			if _, err := h.handleMultiDestWrite(context.Background(),
				map[string]interface{}{"total": 5000}, Operation{Method: "INSERT"}); err != nil {
				t.Fatalf("a destination condition written as %q: %v", condition, err)
			}
			if len(alerts.writes()) != 1 {
				t.Errorf("the destination was skipped although %q is true", condition)
			}

			if _, err := h.handleMultiDestWrite(context.Background(),
				map[string]interface{}{"total": 42}, Operation{Method: "INSERT"}); err != nil {
				t.Fatalf("handleMultiDestWrite: %v", err)
			}
			if len(alerts.writes()) != 1 {
				t.Errorf("the destination was written to although %q is false", condition)
			}
		})
	}
}

func TestRepeatedConnectorsAreToldApartByWhatDistinguishesThem(t *testing.T) {
	// One entry per write, named so a person reading the report knows which is
	// which — and so a failure on one is not hidden by a success on the other.
	db := &recordingWriter{name: "db"}
	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
		{Connector: "db", ConnectorParams: map[string]interface{}{"target": "order_items"}},
		{Connector: "audit", ConnectorParams: map[string]interface{}{"target": "writes"}},
	}, map[string]*recordingWriter{"db": db, "audit": {name: "audit"}})

	result, err := h.handleMultiDestWrite(context.Background(),
		map[string]interface{}{"id": "order-1"}, Operation{Method: "INSERT"})
	if err != nil {
		t.Fatalf("handleMultiDestWrite: %v", err)
	}

	report, _ := result.(*MultiDestResult)
	if report == nil {
		t.Fatal("no report")
	}
	for _, want := range []string{"db:orders", "db:order_items", "audit"} {
		if _, present := report.Results[want]; !present {
			t.Errorf("the report has no %q: %v", want, report.Results)
		}
	}
	// A connector used once keeps its bare name, so nothing reading an
	// existing report has to change.
	if _, present := report.Results["audit"]; !present {
		t.Error("a connector used once was renamed")
	}
}

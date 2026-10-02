package aspect

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// What a flow's transaction captured is `result.captured` to an aspect, in all
// four places that see the result: the `if` condition, `action`, `invalidate`
// and `response`. Each of those used to build `result` on its own with only
// `affected` and `data`, so a value the write computed reached none of them.

func capturedResult(captured map[string]interface{}) *connector.Result {
	return &connector.Result{
		Affected: 1,
		Metadata: map[string]interface{}{"captured": captured},
	}
}

func TestAConditionReadsWhatTheTransactionCaptured(t *testing.T) {
	e := newExecutor(t)
	ctx := context.Background()
	const cond = "has(result.captured.flag_changed) && result.captured.flag_changed == 1"

	tests := []struct {
		name   string
		result *connector.Result
		want   bool
	}{
		{"the flag changed", capturedResult(map[string]interface{}{"flag_changed": int64(1)}), true},
		{"the flag did not change", capturedResult(map[string]interface{}{"flag_changed": int64(0)}), false},

		// Without a transaction there is nothing captured, and the condition
		// is false rather than an evaluation error.
		{"a write that captured nothing", &connector.Result{Affected: 1}, false},
		{"no result at all", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.evaluateCondition(ctx, &Config{Name: "test", If: cond}, map[string]interface{}{}, tc.result, nil, nil)
			if got != tc.want {
				t.Errorf("condition = %v, want %v", got, tc.want)
			}
		})
	}
}

// `result.captured` is bound as an empty map, so asking about it is a question
// with an answer and not an error.
func TestCapturedIsAnEmptyMapWhenNothingWasCaptured(t *testing.T) {
	e := newExecutor(t)
	ctx := context.Background()

	for name, result := range map[string]*connector.Result{
		"a plain write": {Affected: 1},
		"no result":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			if !e.evaluateCondition(ctx, &Config{Name: "test", If: "size(result.captured) == 0"}, map[string]interface{}{}, result, nil, nil) {
				t.Error("result.captured is not an empty map")
			}
		})
	}
}

// What a condition has always seen stays as it was: `data` only when there
// were rows, so `has(result.data)` still tells a read from a write.
func TestAConditionStillSeesDataOnlyWhenThereAreRows(t *testing.T) {
	e := newExecutor(t)
	ctx := context.Background()
	cond := &Config{Name: "test", If: "has(result.data)"}

	if e.evaluateCondition(ctx, cond, map[string]interface{}{}, capturedResult(map[string]interface{}{"x": 1}), nil, nil) {
		t.Error("a write without rows exposes result.data")
	}
	rows := &connector.Result{Rows: []map[string]interface{}{{"id": 1}}}
	if !e.evaluateCondition(ctx, cond, map[string]interface{}{}, rows, nil, nil) {
		t.Error("a result with rows hides result.data")
	}
}

func TestAnActionReadsWhatTheTransactionCaptured(t *testing.T) {
	sink := &recordingWriter{name: "downstream"}
	e := executorWithConnectors(t, map[string]connector.Connector{"downstream": sink})

	err := e.executeAction(context.Background(), &ActionConfig{
		Connector: "downstream",
		Transform: map[string]string{
			"sku":     "input.sku",
			"changed": "result.captured.flag_changed",
		},
	}, map[string]interface{}{"sku": "SKU-1"}, capturedResult(map[string]interface{}{"flag_changed": int64(1)}))
	if err != nil {
		t.Fatalf("action: %v", err)
	}
	if len(sink.written) != 1 {
		t.Fatalf("%d writes, want one", len(sink.written))
	}
	got := sink.written[0].Payload
	if got["sku"] != "SKU-1" || got["changed"] != int64(1) {
		t.Errorf("payload = %#v", got)
	}
}

func TestAnInvalidationKeyReadsWhatTheTransactionCaptured(t *testing.T) {
	e, store := executorWithCache(t)
	ctx := context.Background()

	if err := store.Set(ctx, "product:42", []byte("cached"), 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := e.executeInvalidate(ctx, &InvalidateConfig{
		Storage: "cache",
		Keys:    []string{"product:${result.captured.product_id}"},
	}, map[string]interface{}{}, capturedResult(map[string]interface{}{"product_id": int64(42)})); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, found, _ := store.Get(ctx, "product:42"); found {
		t.Error("the key built from the captured id was not invalidated")
	}
}

func TestAResponseFieldReadsWhatTheTransactionCaptured(t *testing.T) {
	e := newExecutor(t)

	got := e.applyResponseEnrichment(context.Background(), &Config{
		Name:     "echo_id",
		Response: &ResponseConfig{Fields: map[string]string{"product_id": "result.captured.product_id"}},
	}, map[string]interface{}{}, capturedResult(map[string]interface{}{"product_id": int64(42)}))

	if len(got.Rows) != 1 || got.Rows[0]["product_id"] != int64(42) {
		t.Errorf("rows = %#v, want the captured id", got.Rows)
	}
}

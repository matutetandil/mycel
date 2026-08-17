package runtime

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// The key a lock, a semaphore or a coordinate is held under.
//
// It is written as CEL — `"'order:' + input.order_id"` — so that a lock meant
// to serialise work on one order does not serialise work on all of them. It is
// evaluated before the flow body runs, which is earlier than anything that
// creates a CEL evaluator, and the code gave up when there was none: it
// returned the expression itself as the key. Every message then shared one
// key. A per-order lock became a global lock, and a coordinate key never
// matched the signal it was waiting for.

func syncHandler() *FlowHandler {
	// No filter, no accept gate, no dedupe, no transform — nothing that would
	// have created an evaluator before the key is needed. This is an ordinary
	// consumer.
	return &FlowHandler{Config: &flow.Config{
		Name: "process_order",
		From: &flow.FromConfig{Connector: "rabbit"},
	}}
}

func TestASyncKeyIsResolvedPerMessage(t *testing.T) {
	ctx := context.Background()
	handler := syncHandler()

	first := handler.evaluateSyncKey(ctx, `"order:" + input.order_id`,
		map[string]interface{}{"order_id": "order-1"})
	if first != "order:order-1" {
		t.Fatalf("key = %q, want it built from the message", first)
	}

	// The point: two messages must not queue behind one another.
	second := handler.evaluateSyncKey(ctx, `"order:" + input.order_id`,
		map[string]interface{}{"order_id": "order-2"})
	if second == first {
		t.Errorf("two orders share the key %q, so one waits for the other", first)
	}
}

func TestAKeyThatIsJustAName(t *testing.T) {
	// A literal is a legitimate key — one lock for a whole job — and must not
	// be run through the evaluator.
	got := syncHandler().evaluateSyncKey(context.Background(), "nightly_import", nil)
	if got != "nightly_import" {
		t.Errorf("key = %q", got)
	}
	if got := syncHandler().evaluateSyncKey(context.Background(), "", nil); got != "" {
		t.Errorf("an empty key became %q", got)
	}
}

func TestAKeyThatCannotBeEvaluated(t *testing.T) {
	// It falls back to the literal, loudly. That is the safe direction — one
	// key that serialises everything is slow, where an empty key would be no
	// locking at all.
	expression := `"order:" + input.nothing.at.all`
	got := syncHandler().evaluateSyncKey(context.Background(), expression, map[string]interface{}{})
	if got != expression {
		t.Errorf("key = %q, want the literal it fell back to", got)
	}
}

func TestASequenceIsReadFromTheMessage(t *testing.T) {
	// The sequence guard drops a message older than the one already applied,
	// which is what stops a stale update overwriting a fresh one. Read as
	// zero, every message looks like it carries no sequence at all.
	ctx := context.Background()
	handler := syncHandler()

	for name, tc := range map[string]struct {
		input map[string]interface{}
		want  int64
	}{
		"a whole number":      {map[string]interface{}{"version": 42}, 42},
		"one JSON decoded":    {map[string]interface{}{"version": float64(42)}, 42},
		"one written as text": {map[string]interface{}{"version": "42"}, 42},
		"not a number":        {map[string]interface{}{"version": "yesterday"}, 0},
		"nothing at all":      {map[string]interface{}{}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got := handler.evaluateSyncSequence(ctx, "input.version", tc.input)
			if got != tc.want {
				t.Errorf("sequence = %d, want %d", got, tc.want)
			}
		})
	}

	if got := handler.evaluateSyncSequence(ctx, "", map[string]interface{}{"version": 42}); got != 0 {
		t.Errorf("a flow with no sequence expression read %d", got)
	}
}

func TestWhetherASignalFires(t *testing.T) {
	// A signal releases whatever is waiting on its key. Answering "no" when
	// the condition holds leaves the waiting flow to time out; answering
	// "yes" when it does not releases work whose precondition is not met.
	ctx := context.Background()
	handler := syncHandler()

	input := map[string]interface{}{"sku": "SKU-1"}
	output := map[string]interface{}{"status": "created", "count": 3}

	if !handler.evaluateSignalWhen(ctx, `output.status == "created"`, input, output) {
		t.Error("a signal whose condition holds was not emitted")
	}
	if handler.evaluateSignalWhen(ctx, `output.status == "failed"`, input, output) {
		t.Error("a signal was emitted although its condition does not hold")
	}
	// Nothing written means always.
	if !handler.evaluateSignalWhen(ctx, "", input, output) {
		t.Error("a signal with no condition was not emitted")
	}
	// An expression that cannot be evaluated, or one that is not a question,
	// fails closed: a spurious signal releases work that should have waited.
	if handler.evaluateSignalWhen(ctx, `output.nothing.at.all == "x"`, input, output) {
		t.Error("a signal was emitted from a condition that could not be evaluated")
	}
	if handler.evaluateSignalWhen(ctx, `output.count`, input, output) {
		t.Error("a signal was emitted from an expression that is not a yes or no")
	}
}

func TestWhatASignalIsEmittedUnder(t *testing.T) {
	ctx := context.Background()
	handler := syncHandler()

	input := map[string]interface{}{"sku": "SKU-1"}
	output := map[string]interface{}{"parent_id": "parent-1"}

	// The key is built from what the flow produced, which is the whole reason
	// it is evaluated after the body rather than before.
	key, ok := handler.evaluateSignalKey(ctx, `"parent_ready:" + output.parent_id`, input, output)
	if !ok || key != "parent_ready:parent-1" {
		t.Errorf("key = %q, %v", key, ok)
	}

	// A literal works and is almost always a mistake — every flow then emits
	// the same key — but it is not this function's place to refuse it.
	literal, ok := handler.evaluateSignalKey(ctx, "import_done", input, output)
	if !ok || literal != "import_done" {
		t.Errorf("key = %q, %v", literal, ok)
	}

	// Nothing to emit, and something that cannot be evaluated: both say so
	// rather than writing a corrupted key.
	if _, ok := handler.evaluateSignalKey(ctx, "", input, output); ok {
		t.Error("a flow with no signal expression emitted one")
	}
	if _, ok := handler.evaluateSignalKey(ctx, `"x:" + output.nothing.at.all`, input, output); ok {
		t.Error("a key that could not be evaluated was emitted anyway")
	}
}

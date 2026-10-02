package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// A destination marked `required` decides the flow's outcome.
//
// Without it a flow with several destinations fails only when all of them
// fail, and a destination skipped by its condition counts as a success. A
// consumer writing an item to the database and calling two cache services —
// skipped when the relevant field did not change — therefore acked a message
// whose database write had just failed on a lock timeout: no retry, no dead
// letter, and the change was lost. When the calls did run, they evicted the
// caches while the database still held the old value.

var errLockTimeout = errors.New("lock wait timeout exceeded")

func TestAFailedRequiredDestinationFailsTheFlowAndStopsTheOthers(t *testing.T) {
	db := &recordingWriter{name: "db", err: errLockTimeout}
	products := &recordingWriter{name: "products"}
	cms := &recordingWriter{name: "cms", err: nil}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", Required: true, Parallel: true},
		// Parallel, which is the default: declaring them after the required
		// one would not on its own keep them from running alongside it.
		{Connector: "products", Parallel: true},
		{Connector: "cms", Parallel: false},
	}, map[string]*recordingWriter{"db": db, "products": products, "cms": cms})

	_, err := h.writeToAllDestinations(context.Background(), map[string]interface{}{"sku": "A"}, map[string]interface{}{"sku": "A"}, Operation{Method: "POST"})
	if err == nil {
		t.Fatal("the flow succeeded with its required write failed")
	}
	// The original error survives, so error_handling classifies it as what
	// it is rather than as text.
	if !errors.Is(err, errLockTimeout) {
		t.Errorf("err = %v, want it to wrap the destination's own error", err)
	}
	if n := len(products.writes()) + len(cms.writes()); n != 0 {
		t.Errorf("%d other destinations ran after the required one failed", n)
	}
}

func TestARequiredDestinationThatSucceedsLetsTheOthersRun(t *testing.T) {
	db := &recordingWriter{name: "db"}
	products := &recordingWriter{name: "products", err: errors.New("503")}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "products", Parallel: true},
		{Connector: "db", Required: true, Parallel: true},
	}, map[string]*recordingWriter{"db": db, "products": products})

	got, err := h.writeToAllDestinations(context.Background(), map[string]interface{}{}, map[string]interface{}{"sku": "A"}, Operation{Method: "POST"})
	if err != nil {
		t.Fatalf("a failed optional destination failed the flow: %v", err)
	}
	result := got.(*MultiDestResult)
	if _, ok := result.Results["db"]; !ok {
		t.Error("the required destination is not reported")
	}
	if result.Errors["products"] == "" {
		t.Error("the optional destination's failure is not reported")
	}
	if len(products.writes()) != 1 {
		t.Error("the optional destination did not run")
	}
}

// A required destination its condition skipped had nothing to do, which is
// not a failure.
func TestASkippedRequiredDestinationIsNotAFailure(t *testing.T) {
	db := &recordingWriter{name: "db"}
	audit := &recordingWriter{name: "audit"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", Required: true, Parallel: true, When: "input.write == true"},
		{Connector: "audit", Parallel: true},
	}, map[string]*recordingWriter{"db": db, "audit": audit})

	input := map[string]interface{}{"write": false}
	if _, err := h.writeToAllDestinations(context.Background(), input, input, Operation{Method: "POST"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(db.writes()) != 0 || len(audit.writes()) != 1 {
		t.Errorf("db writes = %d, audit writes = %d; want 0 and 1", len(db.writes()), len(audit.writes()))
	}
}

// Without `required`, nothing changes: one success is enough.
func TestWithoutRequiredOneSuccessIsStillEnough(t *testing.T) {
	db := &recordingWriter{name: "db", err: errLockTimeout}
	audit := &recordingWriter{name: "audit"}

	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", Parallel: true},
		{Connector: "audit", Parallel: true},
	}, map[string]*recordingWriter{"db": db, "audit": audit})

	if _, err := h.writeToAllDestinations(context.Background(), map[string]interface{}{}, map[string]interface{}{}, Operation{Method: "POST"}); err != nil {
		t.Fatalf("err = %v, want the existing rule", err)
	}
}

// Two required destinations that both fail are both named.
func TestEveryFailedRequiredDestinationIsNamed(t *testing.T) {
	other := errors.New("connection refused")
	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", Required: true, Parallel: true},
		{Connector: "ledger", Required: true, Parallel: false},
	}, map[string]*recordingWriter{
		"db":     {name: "db", err: errLockTimeout},
		"ledger": {name: "ledger", err: other},
	})

	_, err := h.writeToAllDestinations(context.Background(), map[string]interface{}{}, map[string]interface{}{}, Operation{Method: "POST"})
	if !errors.Is(err, errLockTimeout) || !errors.Is(err, other) {
		t.Errorf("err = %v, want both failures", err)
	}
}

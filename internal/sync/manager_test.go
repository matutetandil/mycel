package sync

import (
	"context"
	"errors"
	"testing"
)

// The sync manager decides which implementation backs a lock, a semaphore or a
// coordinator. Picking the wrong one is not a crash: a flow that asked for a
// Redis lock and silently got an in-memory one still runs, and still looks
// correct on one replica — it just stops being a distributed lock, which is the
// only reason it existed.

func TestGettersDefaultToMemoryAndRejectUnknownDrivers(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	// A nil config, an empty driver and an explicit "memory" all have to mean
	// the same thing, or the same flow behaves differently depending on how
	// completely its storage block was written.
	for _, cfg := range []*SyncStorageConfig{nil, {}, {Driver: "memory"}} {
		if l, err := m.GetLock(ctx, cfg); err != nil || l == nil {
			t.Errorf("GetLock(%+v) = %v, %v; want a memory lock", cfg, l, err)
		}
		if s, err := m.GetSemaphore(ctx, cfg, 3); err != nil || s == nil {
			t.Errorf("GetSemaphore(%+v) = %v, %v", cfg, s, err)
		}
		if c, err := m.GetCoordinator(ctx, cfg); err != nil || c == nil {
			t.Errorf("GetCoordinator(%+v) = %v, %v", cfg, c, err)
		}
		if g, err := m.GetSequenceGuard(ctx, cfg); err != nil || g == nil {
			t.Errorf("GetSequenceGuard(%+v) = %v, %v", cfg, g, err)
		}
	}

	// An unknown driver must be an error rather than a silent fallback to
	// memory — that is the case where a distributed lock quietly becomes a
	// local one.
	bad := &SyncStorageConfig{Driver: "postgres"}
	if _, err := m.GetLock(ctx, bad); err == nil {
		t.Error("GetLock accepted an unsupported driver")
	}
	if _, err := m.GetSemaphore(ctx, bad, 1); err == nil {
		t.Error("GetSemaphore accepted an unsupported driver")
	}
	if _, err := m.GetCoordinator(ctx, bad); err == nil {
		t.Error("GetCoordinator accepted an unsupported driver")
	}
	if _, err := m.GetSequenceGuard(ctx, bad); err == nil {
		t.Error("GetSequenceGuard accepted an unsupported driver")
	}
}

func TestExecuteWithCoordinateWithoutConfigJustRuns(t *testing.T) {
	m := NewManager()
	ran := false
	got, err := m.ExecuteWithCoordinate(context.Background(), nil, nil, "", func() (interface{}, error) {
		ran = true
		return "result", nil
	})
	if err != nil {
		t.Fatalf("ExecuteWithCoordinate: %v", err)
	}
	if !ran || got != "result" {
		t.Errorf("ran=%v result=%v; the function should have run untouched", ran, got)
	}
}

func TestExecuteWithCoordinateSignalsAfterTheFunction(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	cfg := &FlowCoordinateConfig{
		Flow:   "producer",
		Signal: &FlowSignalConfig{Emit: "output.id", TTL: "1m"},
	}

	// The signal key is built from the result, so it can only be computed
	// after the function runs — that ordering is the whole point.
	var sawResult interface{}
	keyFn := func(result interface{}) (string, bool) {
		sawResult = result
		return "order-1", true
	}

	got, err := m.ExecuteWithCoordinate(ctx, cfg, keyFn, "", func() (interface{}, error) {
		return map[string]interface{}{"id": "order-1"}, nil
	})
	if err != nil {
		t.Fatalf("ExecuteWithCoordinate: %v", err)
	}
	if got == nil {
		t.Fatal("the result was swallowed")
	}
	if sawResult == nil {
		t.Error("the signal key builder never saw the result")
	}

	// A waiter must now find the signal already there.
	coord, err := m.GetCoordinator(ctx, nil)
	if err != nil {
		t.Fatalf("GetCoordinator: %v", err)
	}
	ok, err := coord.Wait(ctx, "order-1", 0)
	if err != nil {
		t.Errorf("waiting on the signal errored: %v", err)
	}
	if !ok {
		t.Error("the signal was not emitted, so a waiting flow would block")
	}
}

func TestExecuteWithCoordinateDoesNotSignalWhenTheFunctionFails(t *testing.T) {
	// Signalling after a failure would release a downstream flow to work on
	// something that was never produced.
	m := NewManager()
	ctx := context.Background()
	cfg := &FlowCoordinateConfig{
		Flow:   "producer",
		Signal: &FlowSignalConfig{Emit: "output.id"},
	}

	signalled := false
	_, err := m.ExecuteWithCoordinate(ctx, cfg, func(interface{}) (string, bool) {
		signalled = true
		return "never", true
	}, "", func() (interface{}, error) {
		return nil, errors.New("boom")
	})

	if err == nil {
		t.Error("the failure was swallowed")
	}
	if signalled {
		t.Error("a signal was emitted for a function that failed")
	}
}

func TestExecuteWithCoordinateSkipsAnEmptySignalKey(t *testing.T) {
	// An emit expression that evaluates to nothing must not signal an empty
	// key, which anything waiting on "" would then match.
	m := NewManager()
	cfg := &FlowCoordinateConfig{
		Flow:   "producer",
		Signal: &FlowSignalConfig{Emit: "output.missing"},
	}

	got, err := m.ExecuteWithCoordinate(context.Background(), cfg,
		func(interface{}) (string, bool) { return "", false },
		"", func() (interface{}, error) { return "result", nil })

	// The flow still succeeds — a missing signal key is a warning, not a
	// failure of the work that was already done.
	if err != nil {
		t.Errorf("the flow failed because of a missing signal key: %v", err)
	}
	if got != "result" {
		t.Errorf("result = %v, want it returned untouched", got)
	}
}

func TestCoordinatePreflightCanSkipTheWait(t *testing.T) {
	// The preflight exists so a flow does not block waiting for something that
	// already exists. When it says skip, the function must run immediately.
	m := NewManager()
	cfg := &FlowCoordinateConfig{
		Flow:      "consumer",
		Wait:      &FlowWaitConfig{},
		Timeout:   "1s",
		Preflight: func(context.Context) (bool, error) { return true, nil },
	}

	ran := false
	got, err := m.ExecuteWithCoordinate(context.Background(), cfg, nil, "never-signalled",
		func() (interface{}, error) { ran = true; return "ok", nil })
	if err != nil {
		t.Fatalf("ExecuteWithCoordinate: %v", err)
	}
	if !ran || got != "ok" {
		t.Errorf("ran=%v got=%v; the preflight should have bypassed the wait", ran, got)
	}
}

func TestCoordinatePreflightRejectionAbortsTheFlow(t *testing.T) {
	// ErrPreflightCheckFailed is the explicit policy reject; it has to reach
	// the caller rather than falling through to a wait that will time out.
	m := NewManager()
	cfg := &FlowCoordinateConfig{
		Flow:      "consumer",
		Wait:      &FlowWaitConfig{},
		Timeout:   "1s",
		Preflight: func(context.Context) (bool, error) { return false, ErrPreflightCheckFailed },
	}

	ran := false
	_, err := m.ExecuteWithCoordinate(context.Background(), cfg, nil, "key",
		func() (interface{}, error) { ran = true; return nil, nil })

	if !errors.Is(err, ErrPreflightCheckFailed) {
		t.Errorf("err = %v, want ErrPreflightCheckFailed", err)
	}
	if ran {
		t.Error("the flow body ran despite the preflight rejecting it")
	}
}

func TestStats(t *testing.T) {
	m := NewManager()
	ctx := context.Background()

	// Stats reads the in-memory storages; it must be safe to call before
	// anything has been used, which is when a scrape most likely hits it.
	if s := m.Stats(); s == nil {
		t.Fatal("Stats returned nil on a fresh manager")
	}

	lock, err := m.GetLock(ctx, nil)
	if err != nil {
		t.Fatalf("GetLock: %v", err)
	}
	if acquired, err := lock.Acquire(ctx, "k", 0); err != nil || !acquired {
		t.Fatalf("Acquire = %v, %v", acquired, err)
	}

	stats := m.Stats()
	if _, ok := stats["locks"]; !ok {
		t.Errorf("Stats has no locks entry: %#v", stats)
	}
}

func TestManagerCloseIsSafeWithoutRedis(t *testing.T) {
	// Close walks the Redis clients; with none created it must still be a
	// clean no-op, and calling it twice must not panic.
	m := NewManager()
	if err := m.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

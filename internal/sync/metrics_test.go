package sync

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/metrics"
)

// scrape runs the real Prometheus handler and returns the exposition text, so
// these tests fail if a metric is recorded but never registered — the exact
// gap that left every sync metric permanently absent from /metrics.
func scrape(t *testing.T) string {
	t.Helper()

	rec := httptest.NewRecorder()
	metrics.Default().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

func TestExecuteWithLock_RecordsAcquireAndRelease(t *testing.T) {
	metrics.SetDefault(metrics.NewRegistry("test", "1", "1", "test"))

	mgr := NewManager()
	defer mgr.Close()

	// Purpose left empty on purpose: it must default to "flow" rather than
	// emitting an empty label.
	cfg := &FlowLockConfig{Flow: "sync_inventory", Timeout: "5s", Wait: true}
	if _, err := mgr.ExecuteWithLock(context.Background(), cfg, "sku:12345", func() (interface{}, error) {
		return "ok", nil
	}); err != nil {
		t.Fatalf("ExecuteWithLock: %v", err)
	}

	out := scrape(t)
	for _, want := range []string{
		`mycel_lock_acquired_total{flow="sync_inventory",purpose="flow"} 1`,
		`mycel_lock_released_total{flow="sync_inventory",purpose="flow"} 1`,
		`mycel_lock_held{flow="sync_inventory",purpose="flow"} 0`,
		`mycel_lock_wait_seconds_count{flow="sync_inventory",purpose="flow"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in /metrics output", want)
		}
	}

	// The evaluated key is per entity — one series per SKU would grow without
	// bound, so it must never reach a label.
	if strings.Contains(out, "sku:12345") {
		t.Error("the lock key leaked into a metric label; cardinality is unbounded")
	}
}

func TestExecuteWithLock_RecordsTimeout(t *testing.T) {
	metrics.SetDefault(metrics.NewRegistry("test", "1", "1", "test"))

	mgr := NewManager()
	defer mgr.Close()

	// Hold the key, then have a second caller give up on it. held is closed
	// from inside the critical section, so the contended acquire below cannot
	// start before the lock is actually taken.
	holder := &FlowLockConfig{Flow: "holder", Timeout: "5s", Wait: true}
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = mgr.ExecuteWithLock(context.Background(), holder, "contended", func() (interface{}, error) {
			close(held)
			<-release
			return nil, nil
		})
	}()
	<-held

	waiting := &FlowLockConfig{Flow: "sync_inventory", Timeout: "50ms", Wait: false}
	_, err := mgr.ExecuteWithLock(context.Background(), waiting, "contended", func() (interface{}, error) {
		return nil, errors.New("should not run while the lock is held")
	})
	close(release)
	<-done

	if err == nil {
		t.Fatal("expected the contended acquire to fail")
	}
	if got := scrape(t); !strings.Contains(got, `mycel_lock_timeout_total{flow="sync_inventory",purpose="flow"} 1`) {
		t.Errorf("timeout not recorded: %s", filterLines(got, "mycel_lock"))
	}
}

func TestExecuteWithSemaphore_RecordsAcquireAndRelease(t *testing.T) {
	metrics.SetDefault(metrics.NewRegistry("test", "1", "1", "test"))

	mgr := NewManager()
	defer mgr.Close()

	cfg := &FlowSemaphoreConfig{Flow: "call_erp", MaxPermits: 2, Timeout: "5s", Lease: "30s"}
	if _, err := mgr.ExecuteWithSemaphore(context.Background(), cfg, "erp", func() (interface{}, error) {
		return "ok", nil
	}); err != nil {
		t.Fatalf("ExecuteWithSemaphore: %v", err)
	}

	out := scrape(t)
	for _, want := range []string{
		`mycel_semaphore_acquired_total{flow="call_erp"} 1`,
		`mycel_semaphore_released_total{flow="call_erp"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in /metrics output", want)
		}
	}
}

// filterLines keeps only the lines containing sub, to keep failure output
// readable against a full exposition dump.
func filterLines(s, sub string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// The dedupe critical section and the flow's own lock {} are separate series
// on the same flow. Contention in one means a hot business key; in the other,
// duplicate deliveries piling up.
func TestExecuteWithLock_PurposeSeparatesDedupeFromFlowLock(t *testing.T) {
	metrics.SetDefault(metrics.NewRegistry("test", "1", "1", "test"))

	mgr := NewManager()
	defer mgr.Close()

	run := func(purpose string) {
		cfg := &FlowLockConfig{Flow: "sync_inventory", Purpose: purpose, Timeout: "5s", Wait: true}
		if _, err := mgr.ExecuteWithLock(context.Background(), cfg, "sku:1", func() (interface{}, error) {
			return nil, nil
		}); err != nil {
			t.Fatalf("ExecuteWithLock(%s): %v", purpose, err)
		}
	}
	run(LockPurposeFlow)
	run(LockPurposeDedupe)
	run(LockPurposeDedupe)

	out := scrape(t)
	for _, want := range []string{
		`mycel_lock_acquired_total{flow="sync_inventory",purpose="flow"} 1`,
		`mycel_lock_acquired_total{flow="sync_inventory",purpose="dedupe"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, filterLines(out, "mycel_lock_acquired"))
		}
	}

	// The flow name must stay clean — the purpose belongs in its own label.
	if strings.Contains(out, "(dedupe)") {
		t.Error(`the purpose leaked into the flow label`)
	}
}

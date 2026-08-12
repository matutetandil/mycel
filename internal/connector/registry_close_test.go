package connector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// closeSpy is a connector whose Close can be made to block, so that shutdown
// behaviour can be asserted rather than inferred from timing in production.
type closeSpy struct {
	name    string
	block   time.Duration
	err     error
	closed  atomic.Bool
	started atomic.Bool
}

func (c *closeSpy) Name() string                  { return c.name }
func (c *closeSpy) Type() string                  { return "spy" }
func (c *closeSpy) Connect(context.Context) error { c.started.Store(true); return nil }
func (c *closeSpy) Health(context.Context) error  { return nil }
func (c *closeSpy) Close(ctx context.Context) error {
	if c.block > 0 {
		select {
		case <-time.After(c.block):
		case <-ctx.Done():
		}
	}
	c.closed.Store(true)
	return c.err
}

func TestCloseAllClosesConnectorsConcurrently(t *testing.T) {
	// Sequentially these would cost the sum of the delays; concurrently they
	// cost the longest one. The assertion is deliberately generous — it is
	// there to catch a return to sequential closing, not to measure the
	// scheduler.
	r := NewRegistry()
	for _, name := range []string{"a", "b", "c", "d"} {
		r.Replace(name, &closeSpy{name: name, block: 200 * time.Millisecond})
	}

	start := time.Now()
	if err := r.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 600*time.Millisecond {
		t.Errorf("CloseAll took %v for four 200ms connectors, which looks sequential", elapsed)
	}
}

func TestCloseAllReportsAnErrorWithoutSkippingTheRest(t *testing.T) {
	r := NewRegistry()
	bad := &closeSpy{name: "bad", err: errors.New("boom")}
	good := &closeSpy{name: "good"}
	r.Replace(bad.name, bad)
	r.Replace(good.name, good)

	if err := r.CloseAll(context.Background()); err == nil {
		t.Error("CloseAll returned no error although a connector failed to close")
	}
	// The failure of one must not leave another one open.
	if !good.closed.Load() {
		t.Error("a healthy connector was left open because another one failed")
	}
}

func TestCloseAllHonoursADeadline(t *testing.T) {
	// A connector that would block far longer than the caller is willing to
	// wait must not extend the shutdown: the context is the budget.
	r := NewRegistry()
	r.Replace("slow", &closeSpy{name: "slow", block: 30 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_ = r.CloseAll(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("CloseAll took %v despite a 100ms deadline", elapsed)
	}
}

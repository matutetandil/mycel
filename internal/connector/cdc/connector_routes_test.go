package cdc

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestTwoFlowsCanWatchTheSameChange covers what happens when more than one
// flow is written for the same table and trigger.
//
// Registering the second one over the first would leave a flow that loads,
// validates, and never runs — the failure this project keeps finding. They are
// chained instead, so both see the change.
func TestTwoFlowsCanWatchTheSameChange(t *testing.T) {
	c := New("changes", &Config{Driver: "postgres"}, &flakyListener{failN: 0}, nil)

	var mu sync.Mutex
	var ran []string
	record := func(name string) HandlerFunc {
		return func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, name)
			return nil, nil
		}
	}

	// Written differently on purpose: the trigger is upper-cased and the table
	// lower-cased before matching, so these are the same route.
	c.RegisterRoute("insert:Orders", record("audit"))
	c.RegisterRoute("INSERT:orders", record("notify"))

	if len(c.handlers) != 1 {
		t.Fatalf("registered %d routes, want the two flows on one", len(c.handlers))
	}

	c.dispatchEvent(&Event{
		Trigger: "INSERT",
		Schema:  "public",
		Table:   "orders",
		New:     map[string]interface{}{"id": 1},
	})

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 {
		t.Errorf("flows that ran = %v, want both", ran)
	}
}

// TestClosingAConnectorThatNeverStarted is the shutdown path when a connector
// was built and the service stopped before it began streaming.
func TestClosingAConnectorThatNeverStarted(t *testing.T) {
	c := New("changes", &Config{Driver: "postgres"}, nil, nil)

	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
	if c.started {
		t.Error("a connector that never started was left marked as started")
	}
	// And it must still refuse to claim health.
	if err := c.Health(context.Background()); err == nil {
		t.Error("a connector that never started reported itself healthy")
	}
}

// TestALongHealthyStretchResetsTheWait covers the difference between a stream
// that dropped once after running for hours and one crash-looping.
//
// Without the reset, a service that reconnects fine but drops every night ends
// up waiting the full cap before every reconnect, having learned nothing from
// the hours in between.
func TestALongHealthyStretchResetsTheWait(t *testing.T) {
	fake := &droppingListener{
		// The first run lasts longer than maxBackoff, which is what marks it
		// as healthy; the rest return at once.
		durations: []time.Duration{40 * time.Millisecond, 0, 0},
	}
	c := New("changes", &Config{Driver: "postgres"}, fake, nil)
	c.minBackoff = 5 * time.Millisecond
	c.maxBackoff = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	c.ctx, c.cancel = context.WithCancel(ctx)

	done := make(chan struct{})
	go func() {
		c.superviseListener(make(chan *Event, 4))
		close(done)
	}()

	// Give it enough time for the healthy run and two quick reconnects.
	time.Sleep(200 * time.Millisecond)
	cancel()
	c.cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the supervisor kept running after shutdown")
	}

	if got := fake.startCount(); got < 3 {
		t.Errorf("the listener was started %d times, want it retried after each drop", got)
	}
	if fake.firstWait() > 25*time.Millisecond {
		t.Errorf("waited %v after a healthy stretch, want the short wait again", fake.firstWait())
	}
}

// droppingListener streams for a given time on each call and then returns, as a
// connection that drops does.
type droppingListener struct {
	mu        sync.Mutex
	durations []time.Duration
	calls     int
	lastEnd   time.Time
	firstGap  time.Duration
}

func (d *droppingListener) Start(ctx context.Context, _ chan<- *Event) error {
	d.mu.Lock()
	if d.calls > 0 && d.firstGap == 0 && !d.lastEnd.IsZero() {
		d.firstGap = time.Since(d.lastEnd)
	}
	stay := time.Duration(0)
	if d.calls < len(d.durations) {
		stay = d.durations[d.calls]
	}
	d.calls++
	d.mu.Unlock()

	if stay > 0 {
		select {
		case <-time.After(stay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	d.mu.Lock()
	d.lastEnd = time.Now()
	d.mu.Unlock()
	return context.DeadlineExceeded
}

func (d *droppingListener) Close() error { return nil }

func (d *droppingListener) startCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *droppingListener) firstWait() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.firstGap
}

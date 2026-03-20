package connector

import "sync"

// DebugGate is a reusable semaphore that enforces single-message processing
// when a Studio debugger is connected. The gate starts BLOCKED — no messages
// pass through until the IDE explicitly calls Allow() via debug.consume.
//
// Flow: IDE sends debug.consume → Allow() puts one token → consumer worker
// Acquire() succeeds → message is processed → Release() is a no-op →
// gate blocks again until next Allow().
//
// Zero value is ready to use (gate disabled = no throttling).
type DebugGate struct {
	mu   sync.Mutex
	gate chan struct{}
}

// SetEnabled enables or disables studio-controlled throttling.
// When enabling, a buffered channel of size 1 is created WITHOUT a token —
// the gate starts blocked. The IDE controls flow via Allow().
// Idempotent: calling SetEnabled(true) when already enabled keeps the
// existing channel, avoiding orphaned goroutines blocked on the old one.
// When disabling, the channel is set to nil, unblocking future Acquire calls.
func (g *DebugGate) SetEnabled(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if enabled {
		if g.gate == nil {
			g.gate = make(chan struct{}, 1)
			// No pre-fill: gate starts blocked, IDE controls via Allow()
		}
		// Already enabled — keep the existing channel
	} else {
		g.gate = nil
	}
}

// Allow puts one token in the gate, allowing exactly one message through.
// Called by the debug server when the IDE sends debug.consume.
func (g *DebugGate) Allow() {
	g.mu.Lock()
	gate := g.gate
	g.mu.Unlock()

	if gate != nil {
		select {
		case gate <- struct{}{}:
		default:
		}
	}
}

// Acquire blocks until the gate token is available. Returns immediately if
// the gate is disabled (nil). Call Release after processing the message.
func (g *DebugGate) Acquire() {
	g.mu.Lock()
	gate := g.gate
	g.mu.Unlock()

	if gate != nil {
		<-gate
	}
}

// Release is a no-op when the gate is enabled (studio mode).
// The IDE controls the next message via Allow(). When the gate is disabled,
// this is also a no-op since Acquire doesn't block.
func (g *DebugGate) Release() {
	// In studio mode, don't put the token back.
	// The next message waits for the IDE to call Allow().
}

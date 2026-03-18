package connector

import "sync"

// DebugGate is a reusable semaphore that enforces single-message processing
// when a debugger is connected. When enabled, only one goroutine can hold the
// gate at a time; others block until it is released.
//
// Zero value is ready to use (gate disabled = no throttling).
type DebugGate struct {
	mu   sync.Mutex
	gate chan struct{}
}

// SetEnabled enables or disables single-message throttling.
// When enabling, a buffered channel of size 1 is created with one pre-filled token.
// When disabling, the channel is set to nil, unblocking future Acquire calls.
func (g *DebugGate) SetEnabled(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if enabled {
		g.gate = make(chan struct{}, 1)
		g.gate <- struct{}{} // pre-fill: first message can proceed immediately
	} else {
		g.gate = nil
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

// Release returns the token so the next message can proceed.
// No-op if the gate is disabled.
func (g *DebugGate) Release() {
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

package flow

import (
	"sync"
	"time"
)

// RequeueTracker tracks requeue attempts for filtered messages to prevent
// infinite requeue loops. Thread-safe with automatic TTL-based cleanup.
type RequeueTracker struct {
	mu      sync.Mutex
	counts  map[string]*requeueEntry
	ttl     time.Duration
	stopCh  chan struct{}
	stopped bool
}

type requeueEntry struct {
	count   int
	expires time.Time
}

// NewRequeueTracker creates a new tracker with the given TTL for entries.
// Starts a background goroutine that cleans up expired entries every ttl/2.
func NewRequeueTracker(ttl time.Duration) *RequeueTracker {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	t := &RequeueTracker{
		counts: make(map[string]*requeueEntry),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}

	go t.cleanupLoop()
	return t
}

// IncrementAndCheck increments the requeue count for the given message ID
// and returns the current count and whether the message should be ACKed
// (i.e., max requeue attempts reached).
func (t *RequeueTracker) IncrementAndCheck(id string, maxRequeue int) (count int, shouldAck bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.counts[id]
	if !ok {
		entry = &requeueEntry{}
		t.counts[id] = entry
	}

	entry.count++
	entry.expires = time.Now().Add(t.ttl)

	if maxRequeue <= 0 {
		maxRequeue = 3
	}

	return entry.count, entry.count >= maxRequeue
}

// Count returns the current requeue count for a message ID.
func (t *RequeueTracker) Count(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	if entry, ok := t.counts[id]; ok {
		return entry.count
	}
	return 0
}

// Stop stops the background cleanup goroutine.
func (t *RequeueTracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.stopped {
		t.stopped = true
		close(t.stopCh)
	}
}

// cleanupLoop periodically removes expired entries.
func (t *RequeueTracker) cleanupLoop() {
	interval := t.ttl / 2
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.cleanup()
		}
	}
}

// cleanup removes expired entries.
func (t *RequeueTracker) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for id, entry := range t.counts {
		if now.After(entry.expires) {
			delete(t.counts, id)
		}
	}
}

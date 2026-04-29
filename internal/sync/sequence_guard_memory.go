package sync

import (
	"context"
	gosync "sync"
	"time"
)

// MemorySequenceGuard is the in-memory implementation of SequenceGuard.
// Suitable for tests and single-process deployments. Distributed setups
// should use the Redis backend.
type MemorySequenceGuard struct {
	mu      gosync.RWMutex
	entries map[string]memorySeqEntry
	stop    chan struct{}
	stopped bool
}

type memorySeqEntry struct {
	sequence  int64
	expiresAt time.Time // zero = no expiry
}

// NewMemorySequenceGuard creates a new in-memory sequence guard. A
// background sweeper expires entries every minute; pass a non-zero
// reapInterval to override.
func NewMemorySequenceGuard(reapInterval time.Duration) *MemorySequenceGuard {
	if reapInterval <= 0 {
		reapInterval = time.Minute
	}
	g := &MemorySequenceGuard{
		entries: make(map[string]memorySeqEntry),
		stop:    make(chan struct{}),
	}
	go g.reaper(reapInterval)
	return g
}

func (g *MemorySequenceGuard) reaper(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-g.stop:
			return
		case now := <-t.C:
			g.expire(now)
		}
	}
}

func (g *MemorySequenceGuard) expire(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, e := range g.entries {
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			delete(g.entries, k)
		}
	}
}

// Read implements SequenceGuard.Read.
func (g *MemorySequenceGuard) Read(ctx context.Context, key string) (int64, bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	e, ok := g.entries[key]
	if !ok {
		return 0, false, nil
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		return 0, false, nil
	}
	return e.sequence, true, nil
}

// Write implements SequenceGuard.Write.
func (g *MemorySequenceGuard) Write(ctx context.Context, key string, sequence int64, ttl time.Duration) error {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entries[key] = memorySeqEntry{sequence: sequence, expiresAt: exp}
	return nil
}

// Close stops the reaper goroutine.
func (g *MemorySequenceGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return nil
	}
	g.stopped = true
	close(g.stop)
	return nil
}

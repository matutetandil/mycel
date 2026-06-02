package sync

import (
	"context"
	"testing"
)

// TestGetCoordinator_RedisIsCachedNotPerCall guards the goroutine leak fixed in
// v2.7.x: GetCoordinator used to return a fresh RedisCoordinator on every call,
// and each one PSubscribes (holding a pooled Redis connection) and spawns a
// listener goroutine that is never stopped. Called once per ExecuteWithCoordinate
// (i.e. once per message), that leaked ~2-3 goroutines + a connection per message.
//
// The coordinator is a long-lived Pub/Sub hub by design, so it must be shared.
// This test asserts GetCoordinator returns the SAME cached instance across calls.
// (Uses a closed local port: redis.NewClient is lazy and the subscription only
// connects in the background, so no live Redis is needed for the identity check.)
func TestGetCoordinator_RedisIsCachedNotPerCall(t *testing.T) {
	m := NewManager()
	t.Cleanup(func() { _ = m.Close() })

	cfg := &SyncStorageConfig{Driver: "redis", Host: "127.0.0.1", Port: 6399, DB: 0}

	c1, err := m.GetCoordinator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("GetCoordinator (1): %v", err)
	}
	c2, err := m.GetCoordinator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("GetCoordinator (2): %v", err)
	}

	rc1, ok1 := c1.(*RedisCoordinator)
	rc2, ok2 := c2.(*RedisCoordinator)
	if !ok1 || !ok2 {
		t.Fatalf("expected *RedisCoordinator, got %T and %T", c1, c2)
	}
	if rc1 != rc2 {
		t.Fatal("GetCoordinator returned two distinct coordinators for the same storage; " +
			"each call PSubscribes + spawns a listener goroutine, so per-call creation leaks " +
			"a goroutine and a Redis connection per message")
	}

	// A different storage target must still get its own coordinator.
	cfgB := &SyncStorageConfig{Driver: "redis", Host: "127.0.0.1", Port: 6400, DB: 0}
	c3, err := m.GetCoordinator(context.Background(), cfgB)
	if err != nil {
		t.Fatalf("GetCoordinator (3): %v", err)
	}
	if rc3, _ := c3.(*RedisCoordinator); rc3 == rc1 {
		t.Fatal("distinct storage targets must not share a coordinator")
	}
}

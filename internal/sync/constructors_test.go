package sync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// Building the primitives from configuration, and the defaults a flow gets when
// it says nothing. A default that is wrong here is not visible in any single
// run: it shows up as a lock nobody waits for, or one nobody lets go of.

func TestALockSaysHowLongItWaitsAndHowLongItKeeps(t *testing.T) {
	cfg := DefaultLockConfig()

	// Both have to be set. Zero would mean either "give up immediately",
	// which serialises nothing, or "hold for ever", which is a consumer that
	// stops after the first message that dies mid-flight.
	if cfg.Timeout <= 0 {
		t.Errorf("timeout = %v, want a bound on how long a holder keeps it", cfg.Timeout)
	}
	// And waiting is the default: failing immediately would turn every
	// contended message into an error rather than a queued one.
	if !cfg.Wait {
		t.Error("a flow taking a lock gives up rather than waiting for it")
	}
	if cfg.Retry <= 0 {
		t.Errorf("retry = %v, want an interval between attempts", cfg.Retry)
	}
}

func TestASemaphoreLetsSomebodyThroughByDefault(t *testing.T) {
	cfg := DefaultSemaphoreConfig()

	// A default of zero permits would stop every flow that declares one.
	if cfg.MaxPermits < 1 {
		t.Errorf("permits = %d, want at least one", cfg.MaxPermits)
	}
	if cfg.Timeout <= 0 {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
}

func TestCoordinationWaitsAndThenGivesUp(t *testing.T) {
	cfg := DefaultCoordinateConfig()

	// Waiting for ever is the failure that empties a queue into memory: every
	// message waiting on a signal that never comes.
	if cfg.Timeout <= 0 {
		t.Errorf("timeout = %v, want a bound", cfg.Timeout)
	}
}

// --- The backends a deployment actually runs on -----------------------------

func TestTheRedisPrimitivesConnectWhereTheyAreTold(t *testing.T) {
	// Each of these pings on the way up, so a wrong address is a startup
	// failure rather than a lock that silently never locks.
	server := miniredis.RunT(t)
	url := "redis://" + server.Addr()

	lock, err := NewRedisLock(&RedisLockConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisLock: %v", err)
	}
	stats, err := lock.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats["instance_id"] == "" {
		t.Error("the lock does not know which instance holds it")
	}

	semaphore, err := NewRedisSemaphore(&RedisSemaphoreConfig{URL: url, MaxPermits: 3})
	if err != nil {
		t.Fatalf("NewRedisSemaphore: %v", err)
	}
	if _, err := semaphore.Stats(context.Background(), "orders"); err != nil {
		t.Errorf("Stats: %v", err)
	}
	// Changing the limit without a restart is what a deployment does when a
	// downstream service is being protected.
	semaphore.SetMaxPermits(5)
	if err := semaphore.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	coordinator, err := NewRedisCoordinator(&RedisCoordinatorConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisCoordinator: %v", err)
	}
	if _, err := coordinator.Stats(context.Background()); err != nil {
		t.Errorf("Stats: %v", err)
	}
}

func TestARedisAddressThatIsNotOneIsRefused(t *testing.T) {
	// At startup, where it can be fixed — not as a primitive that quietly
	// coordinates nothing.
	if _, err := NewRedisLock(&RedisLockConfig{URL: "not-a-url"}); err == nil {
		t.Error("a lock was built from something that is not an address")
	}
	if _, err := NewRedisSemaphore(&RedisSemaphoreConfig{URL: "not-a-url", MaxPermits: 1}); err == nil {
		t.Error("a semaphore was built from something that is not an address")
	}
	if _, err := NewRedisCoordinator(&RedisCoordinatorConfig{URL: "not-a-url"}); err == nil {
		t.Error("a coordinator was built from something that is not an address")
	}

	// And an address with nothing behind it, which is the ordinary case: the
	// Redis in the configuration is not the one that is running.
	if _, err := NewRedisLock(&RedisLockConfig{URL: "redis://127.0.0.1:1"}); err == nil {
		t.Error("a lock was built against a Redis nobody is running")
	}
}

func TestKeysAreNamespacedSoTwoServicesDoNotShareALock(t *testing.T) {
	// Two services on one Redis, both locking on "order-1", must not be
	// waiting on each other — that is a deadlock between systems that have
	// nothing to do with each other.
	server := miniredis.RunT(t)
	url := "redis://" + server.Addr()

	lock, err := NewRedisLock(&RedisLockConfig{URL: url, Prefix: "orders:lock:"})
	if err != nil {
		t.Fatalf("NewRedisLock: %v", err)
	}
	if !strings.HasPrefix(lock.prefix, "orders:") {
		t.Errorf("prefix = %q, want the one configured", lock.prefix)
	}

	// And a default, so keys never collide with whatever else is in there.
	plain, err := NewRedisLock(&RedisLockConfig{URL: url})
	if err != nil {
		t.Fatalf("NewRedisLock: %v", err)
	}
	if plain.prefix == "" {
		t.Error("locks are stored under bare keys, which collide with anything else on this Redis")
	}
}

// --- What a lock in memory forgets ------------------------------------------

func TestALockNobodyReleasedDoesNotLeak(t *testing.T) {
	// A holder that dies mid-flight leaves the entry behind. Without the sweep
	// the map grows for the life of the process — and a busy consumer takes a
	// lock per message.
	storage := NewMemoryLockStorage()

	storage.mu.Lock()
	storage.locks["gone"] = &lockEntry{expiresAt: time.Now().Add(-time.Minute)}
	storage.locks["held"] = &lockEntry{expiresAt: time.Now().Add(time.Minute)}
	storage.mu.Unlock()

	storage.cleanExpired()

	storage.mu.RLock()
	defer storage.mu.RUnlock()
	if _, ok := storage.locks["gone"]; ok {
		t.Error("a lock whose holder is gone is still held")
	}
	if _, ok := storage.locks["held"]; !ok {
		t.Error("a lock somebody is holding was taken away")
	}
}

package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// A lock kept in memory serialises work inside one process, which is not the
// question a consumer is asking. Two replicas of a service, each with its own
// memory, need the lock to live where they can both see it — so the Redis
// backend is the one a deployment actually runs on, and it was the one with no
// tests at all.
//
// What a distributed lock gets wrong is not visible from inside: both holders
// believe they have it, both process the message, and the duplicate turns up
// downstream as two orders or a doubled balance.

func redisServer(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestOnlyOneHolderAtATime(t *testing.T) {
	// The whole point. Two replicas asking for the same key: one gets it, and
	// the other is told it did not.
	_, client := redisServer(t)
	first := NewRedisLockFromClient(client, "mycel:lock:")
	second := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	got, err := first.Acquire(ctx, "order-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !got {
		t.Fatal("the first replica could not take a free lock")
	}

	got, err = second.Acquire(ctx, "order-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got {
		t.Error("a second replica took a lock that was already held")
	}
}

func TestReleasingLetsTheNextOneIn(t *testing.T) {
	_, client := redisServer(t)
	first := NewRedisLockFromClient(client, "mycel:lock:")
	second := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	if _, err := first.Acquire(ctx, "order-1", time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := first.Release(ctx, "order-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	got, err := second.Acquire(ctx, "order-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !got {
		t.Error("a released lock was not free")
	}
}

func TestOneReplicaCannotReleaseAnothersLock(t *testing.T) {
	// The reason release is a compare-and-delete rather than a delete. A
	// replica whose own lock expired, releasing on its way out, would
	// otherwise hand away the lock the next holder is working under — and
	// then a third takes it while the second is still going.
	_, client := redisServer(t)
	holder := NewRedisLockFromClient(client, "mycel:lock:")
	other := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	if _, err := holder.Acquire(ctx, "order-1", time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	err := other.Release(ctx, "order-1")
	if err == nil {
		t.Fatal("a replica released a lock it did not hold")
	}
	if !errors.Is(err, ErrLockNotHeld) {
		t.Errorf("error = %v, want it to say the lock is not held", err)
	}

	// And the real holder still has it.
	held, err := holder.IsHeld(ctx, "order-1")
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if !held {
		t.Error("the holder lost its lock to somebody else's release")
	}
}

func TestReleasingSomethingNobodyHoldsIsReported(t *testing.T) {
	_, client := redisServer(t)
	lock := NewRedisLockFromClient(client, "mycel:lock:")

	if err := lock.Release(context.Background(), "never-taken"); !errors.Is(err, ErrLockNotHeld) {
		t.Errorf("error = %v, want it to say the lock is not held", err)
	}
}

func TestALockExpiresOnItsOwn(t *testing.T) {
	// The replica holding it may be gone — killed mid-message, its pod
	// evicted. Without an expiry the key stays for ever and the queue stops.
	server, client := redisServer(t)
	first := NewRedisLockFromClient(client, "mycel:lock:")
	second := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	if _, err := first.Acquire(ctx, "order-1", 30*time.Second); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	server.FastForward(31 * time.Second)

	got, err := second.Acquire(ctx, "order-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !got {
		t.Error("a lock whose holder is gone was never released")
	}
}

func TestALockCanBeHeldLongerThanItWasTaken(t *testing.T) {
	// Work that outlives the lock's timeout is the case the heartbeat exists
	// for: extending keeps the lock rather than letting a second replica in
	// while the first is still writing.
	server, client := redisServer(t)
	holder := NewRedisLockFromClient(client, "mycel:lock:")
	other := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	if _, err := holder.Acquire(ctx, "order-1", 30*time.Second); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	server.FastForward(20 * time.Second)
	extended, err := holder.Extend(ctx, "order-1", 30*time.Second)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if !extended {
		t.Fatal("the holder could not extend its own lock")
	}

	// Past the original expiry, still held.
	server.FastForward(20 * time.Second)
	got, acquireErr := other.Acquire(ctx, "order-1", time.Minute)
	if acquireErr != nil {
		t.Fatalf("Acquire: %v", acquireErr)
	}
	if got {
		t.Error("a lock that was extended was taken by somebody else")
	}
}

func TestOnlyTheHolderCanExtend(t *testing.T) {
	// Otherwise a replica could keep alive a lock it does not hold, and the
	// real holder's release would find it gone.
	_, client := redisServer(t)
	holder := NewRedisLockFromClient(client, "mycel:lock:")
	other := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	if _, err := holder.Acquire(ctx, "order-1", 30*time.Second); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	extended, err := other.Extend(ctx, "order-1", time.Hour)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if extended {
		t.Error("a replica extended a lock it did not hold")
	}
}

func TestExtendingSomethingNobodyHoldsIsReported(t *testing.T) {
	_, client := redisServer(t)
	lock := NewRedisLockFromClient(client, "mycel:lock:")
	extended, err := lock.Extend(context.Background(), "never-taken", time.Minute)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if extended {
		t.Error("a lock nobody holds was extended")
	}
}

func TestOnlyTheHolderReportsHoldingIt(t *testing.T) {
	_, client := redisServer(t)
	holder := NewRedisLockFromClient(client, "mycel:lock:")
	other := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	if _, err := holder.Acquire(ctx, "order-1", time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	held, err := holder.IsHeld(ctx, "order-1")
	if err != nil || !held {
		t.Errorf("the holder reports held=%v err=%v", held, err)
	}

	held, err = other.IsHeld(ctx, "order-1")
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if held {
		t.Error("a replica reported holding somebody else's lock")
	}

	held, err = holder.IsHeld(ctx, "never-taken")
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if held {
		t.Error("a lock nobody has taken was reported as held")
	}
}

func TestDifferentKeysDoNotBlockEachOther(t *testing.T) {
	// A lock per order means orders are processed in parallel; one key
	// blocking another would serialise the whole consumer.
	_, client := redisServer(t)
	lock := NewRedisLockFromClient(client, "mycel:lock:")
	ctx := context.Background()

	for _, key := range []string{"order-1", "order-2", "order-3"} {
		got, err := lock.Acquire(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("Acquire %s: %v", key, err)
		}
		if !got {
			t.Errorf("%s was blocked by another key", key)
		}
	}
}

func TestThePrefixKeepsTwoServicesApart(t *testing.T) {
	// Two services sharing one Redis, each locking on an order id, must not
	// block each other.
	server, client := redisServer(t)
	orders := NewRedisLockFromClient(client, "orders:lock:")
	billing := NewRedisLockFromClient(client, "billing:lock:")
	ctx := context.Background()

	if _, err := orders.Acquire(ctx, "1", time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	got, err := billing.Acquire(ctx, "1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !got {
		t.Error("another service's lock blocked this one")
	}

	if _, err := server.Get("orders:lock:1"); err != nil {
		t.Errorf("the key was not written under the prefix: %v", err)
	}
}

func TestADefaultPrefixIsApplied(t *testing.T) {
	// So that a lock never lands on a bare key in a Redis somebody else is
	// also using.
	server, client := redisServer(t)
	lock := NewRedisLockFromClient(client, "")
	if _, err := lock.Acquire(context.Background(), "order-1", time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := server.Get("mycel:lock:order-1"); err != nil {
		t.Errorf("the key was not written under the default prefix: %v", err)
	}
}

func TestALockWhoseRedisIsGoneReportsIt(t *testing.T) {
	// Rather than reporting the lock as taken, which would let two replicas
	// through the moment the store became unreachable.
	server, client := redisServer(t)
	lock := NewRedisLockFromClient(client, "mycel:lock:")
	server.Close()

	got, err := lock.Acquire(context.Background(), "order-1", time.Minute)
	if err == nil {
		t.Fatal("a lock was taken against a Redis that is gone")
	}
	if got {
		t.Error("the lock was reported as acquired")
	}
}

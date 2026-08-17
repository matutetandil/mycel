package sync

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The other two primitives a deployment shares across replicas, and the other
// two with no tests on the backend that makes them shared.

// A semaphore is a lock that lets several through: the number of calls a
// downstream service will take at once, counted across every replica rather
// than per process. Counting per process means ten replicas of "at most three"
// send thirty.

func TestPermitsRunOutAtTheLimit(t *testing.T) {
	_, client := redisServer(t)
	semaphore := NewRedisSemaphoreFromClient(client, "mycel:sem:", 3)
	ctx := context.Background()

	var held []string
	for i := 0; i < 3; i++ {
		permit, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute)
		if err != nil {
			t.Fatalf("permit %d: %v", i+1, err)
		}
		held = append(held, permit)
	}

	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); !errors.Is(err, ErrSemaphoreFull) {
		t.Errorf("a fourth caller got through a limit of three: %v", err)
	}

	// Every permit is its own, so releasing one is releasing that one.
	for i, permit := range held {
		if permit == "" {
			t.Errorf("permit %d has no identity, so it cannot be released", i)
		}
		for j, other := range held {
			if i != j && permit == other {
				t.Errorf("permits %d and %d are the same", i, j)
			}
		}
	}
}

func TestReleasingAPermitLetsTheNextCallerIn(t *testing.T) {
	_, client := redisServer(t)
	semaphore := NewRedisSemaphoreFromClient(client, "mycel:sem:", 1)
	ctx := context.Background()

	permit, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); !errors.Is(err, ErrSemaphoreFull) {
		t.Fatalf("the limit was not applied: %v", err)
	}

	if err := semaphore.Release(ctx, "magento", permit); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); err != nil {
		t.Errorf("a released permit was not free: %v", err)
	}
}

func TestReleasingAPermitNobodyHoldsIsReported(t *testing.T) {
	// A release with the wrong identifier must not free somebody else's
	// permit, which would put two callers through one slot.
	_, client := redisServer(t)
	semaphore := NewRedisSemaphoreFromClient(client, "mycel:sem:", 1)
	ctx := context.Background()

	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := semaphore.Release(ctx, "magento", "not-a-permit"); !errors.Is(err, ErrPermitNotFound) {
		t.Errorf("error = %v, want the release refused", err)
	}
	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); !errors.Is(err, ErrSemaphoreFull) {
		t.Error("a wrong release freed the permit somebody else was holding")
	}
}

func TestAPermitIsGivenUpWhenItsHolderIsGone(t *testing.T) {
	// A replica killed mid-call never releases. Without the lease, the permit
	// is lost for ever and the limit shrinks by one every time it happens —
	// until nothing gets through at all.
	_, client := redisServer(t)
	semaphore := NewRedisSemaphoreFromClient(client, "mycel:sem:", 1)
	ctx := context.Background()

	if _, err := semaphore.Acquire(ctx, "magento", time.Second, 50*time.Millisecond); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); err != nil {
		t.Errorf("a permit whose holder is gone was never reclaimed: %v", err)
	}
}

func TestTheCountOfFreePermitsIsReported(t *testing.T) {
	_, client := redisServer(t)
	semaphore := NewRedisSemaphoreFromClient(client, "mycel:sem:", 3)
	ctx := context.Background()

	free, err := semaphore.Available(ctx, "magento")
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if free != 3 {
		t.Errorf("free = %d, want all of them", free)
	}

	permit, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if free, _ = semaphore.Available(ctx, "magento"); free != 2 {
		t.Errorf("free = %d after taking one of three", free)
	}

	if err := semaphore.Release(ctx, "magento", permit); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if free, _ = semaphore.Available(ctx, "magento"); free != 3 {
		t.Errorf("free = %d after giving it back", free)
	}
}

func TestTwoKeysHaveTheirOwnPermits(t *testing.T) {
	_, client := redisServer(t)
	semaphore := NewRedisSemaphoreFromClient(client, "mycel:sem:", 1)
	ctx := context.Background()

	if _, err := semaphore.Acquire(ctx, "magento", time.Second, time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := semaphore.Acquire(ctx, "sap", time.Second, time.Minute); err != nil {
		t.Errorf("one destination's limit blocked another: %v", err)
	}
}

// A sequence guard drops a message older than the one already applied. It is
// what keeps an out-of-order redelivery from overwriting a newer record with
// an older one — a stale price, a reverted stock level — which is a corruption
// nobody notices until somebody reads the number.

func TestNothingIsStoredForAKeyNobodyHasSeen(t *testing.T) {
	_, client := redisServer(t)
	guard := NewRedisSequenceGuardFromClient(client, "mycel:seq:")

	_, found, err := guard.Read(context.Background(), "product-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Error("a key nobody has seen reported a stored sequence")
	}
}

func TestASequenceComesBackAsItWentIn(t *testing.T) {
	_, client := redisServer(t)
	guard := NewRedisSequenceGuardFromClient(client, "mycel:seq:")
	ctx := context.Background()

	// Sequences are often timestamps in milliseconds, which is past what a
	// float holds exactly — so the type has to survive the round trip.
	const sequence = int64(1755012345678)
	if err := guard.Write(ctx, "product-1", sequence, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, found, err := guard.Read(ctx, "product-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found {
		t.Fatal("what was just written was not found")
	}
	if got != sequence {
		t.Errorf("sequence = %d, want %d", got, sequence)
	}
}

func TestAStoredSequenceIsForgottenWhenItsTimeIsUp(t *testing.T) {
	// The guard remembers per key, so without an expiry the memory grows with
	// every product that was ever updated.
	server, client := redisServer(t)
	guard := NewRedisSequenceGuardFromClient(client, "mycel:seq:")
	ctx := context.Background()

	if err := guard.Write(ctx, "product-1", 5, time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}
	server.FastForward(2 * time.Minute)

	if _, found, _ := guard.Read(ctx, "product-1"); found {
		t.Error("a stored sequence outlived its expiry")
	}
}

func TestSomethingThatIsNotASequenceIsTreatedAsNone(t *testing.T) {
	// A value hand-edited in Redis, or written by something else under the
	// same key. Failing the flow over it would stop the queue; treating it as
	// unseen lets the next message write a good value over it.
	server, client := redisServer(t)
	guard := NewRedisSequenceGuardFromClient(client, "mycel:seq:")

	if err := server.Set("mycel:seq:product-1", "not a number"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, found, err := guard.Read(context.Background(), "product-1")
	if err != nil {
		t.Errorf("a corrupt value failed the read: %v", err)
	}
	if found || got != 0 {
		t.Errorf("read %d found=%v, want it treated as nothing stored", got, found)
	}
}

func TestEachKeyRemembersItsOwnSequence(t *testing.T) {
	_, client := redisServer(t)
	guard := NewRedisSequenceGuardFromClient(client, "mycel:seq:")
	ctx := context.Background()

	if err := guard.Write(ctx, "product-1", 10, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := guard.Write(ctx, "product-2", 20, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for key, want := range map[string]int64{"product-1": 10, "product-2": 20} {
		got, found, err := guard.Read(ctx, key)
		if err != nil || !found {
			t.Fatalf("%s: found=%v err=%v", key, found, err)
		}
		if got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
}

func TestAGuardWhoseRedisIsGoneReportsIt(t *testing.T) {
	// Reading "nothing stored" from an unreachable Redis would let every
	// message through as if it were the newest, which is the corruption the
	// guard exists to prevent.
	server, client := redisServer(t)
	guard := NewRedisSequenceGuardFromClient(client, "mycel:seq:")
	server.Close()

	if _, _, err := guard.Read(context.Background(), "product-1"); err == nil {
		t.Error("a guard read against a Redis that is gone reported nothing stored")
	}
	if err := guard.Write(context.Background(), "product-1", 5, time.Hour); err == nil {
		t.Error("a write against a Redis that is gone reported success")
	}
}

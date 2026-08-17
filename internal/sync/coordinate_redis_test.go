package sync

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// A coordinate block holds a message until something else has happened —
// inventory waits for the product it refers to, an invoice waits for its order.
// Across replicas that "something else" happened in another process, so the
// signal lives in Redis and the waiting is a subscription. None of it had tests.
//
// The failure that matters is the one nobody sees: a waiter that is never woken
// sits until its timeout and the message is dropped or requeued, which looks
// like the other side being slow rather than a signal that went missing.

func coordinators(t *testing.T) (*RedisCoordinator, *RedisCoordinator) {
	t.Helper()
	server, client := redisServer(t)

	other := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = other.Close() })

	first := NewRedisCoordinatorFromClient(client, "mycel:coord:")
	second := NewRedisCoordinatorFromClient(other, "mycel:coord:")
	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})
	return first, second
}

func TestAWaiterIsWokenByASignalFromAnotherReplica(t *testing.T) {
	waiter, signaller := coordinators(t)

	woken := make(chan bool, 1)
	go func() {
		got, err := waiter.Wait(context.Background(), "product-1", 5*time.Second)
		if err != nil {
			t.Errorf("Wait: %v", err)
		}
		woken <- got
	}()

	// Give the waiter time to register before the signal is sent, which is the
	// ordering the double-check inside Wait exists to make safe either way.
	time.Sleep(50 * time.Millisecond)
	if err := signaller.Signal(context.Background(), "product-1", time.Minute); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	select {
	case got := <-woken:
		if !got {
			t.Error("the waiter gave up although the signal was sent")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the signal never reached the waiter")
	}
}

func TestASignalThatArrivedFirstIsNotMissed(t *testing.T) {
	// The message that turns up after the thing it was waiting for. Without
	// the check before waiting it would wait for a signal that has already
	// been and gone.
	waiter, signaller := coordinators(t)

	if err := signaller.Signal(context.Background(), "product-2", time.Minute); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	got, err := waiter.Wait(context.Background(), "product-2", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !got {
		t.Error("a signal that had already been sent was waited for all over again")
	}
}

func TestWaitingForSomethingThatNeverHappensGivesUp(t *testing.T) {
	// And says it gave up, rather than reporting the signal as received: the
	// flow decides between requeueing and dead-lettering on this answer.
	waiter, _ := coordinators(t)

	start := time.Now()
	got, err := waiter.Wait(context.Background(), "never", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got {
		t.Error("a signal nobody sent was reported as received")
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("it gave up after %v, before the timeout it was given", elapsed)
	}
}

func TestAWaiterStopsWhenItsMessageIsCancelled(t *testing.T) {
	// A consumer shutting down must not hold its goroutines until every
	// timeout runs out.
	waiter, _ := coordinators(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := waiter.Wait(ctx, "never", 30*time.Second)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled wait reported no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wait carried on after its message was cancelled")
	}
}

func TestSeveralWaitersForOneSignalAreAllWoken(t *testing.T) {
	// Everything held up by one product is released by one signal; waking only
	// the first would leave the rest to time out.
	waiter, signaller := coordinators(t)

	const waiters = 5
	woken := make(chan bool, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			got, _ := waiter.Wait(context.Background(), "product-3", 5*time.Second)
			woken <- got
		}()
	}
	time.Sleep(100 * time.Millisecond)

	if err := signaller.Signal(context.Background(), "product-3", time.Minute); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	for i := 0; i < waiters; i++ {
		select {
		case got := <-woken:
			if !got {
				t.Fatal("a waiter gave up although the signal was sent")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d waiters were woken", i, waiters)
		}
	}
}

func TestOneSignalDoesNotWakeWaitersForAnother(t *testing.T) {
	waiter, signaller := coordinators(t)

	woken := make(chan bool, 1)
	go func() {
		got, _ := waiter.Wait(context.Background(), "product-4", 400*time.Millisecond)
		woken <- got
	}()
	time.Sleep(50 * time.Millisecond)

	if err := signaller.Signal(context.Background(), "something-else", time.Minute); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	if got := <-woken; got {
		t.Error("a waiter was woken by a signal it was not waiting for")
	}
}

func TestASignalIsForgottenWhenItsTimeIsUp(t *testing.T) {
	// The signal outliving its purpose would let a message from next week
	// through on last week's event.
	server, client := redisServer(t)
	coordinator := NewRedisCoordinatorFromClient(client, "mycel:coord:")
	defer func() { _ = coordinator.Close() }()
	ctx := context.Background()

	if err := coordinator.Signal(ctx, "product-5", 30*time.Second); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	if exists, _ := coordinator.Exists(ctx, "product-5"); !exists {
		t.Fatal("a signal that was just sent does not exist")
	}

	server.FastForward(31 * time.Second)

	if exists, _ := coordinator.Exists(ctx, "product-5"); exists {
		t.Error("a signal outlived the time it was given")
	}
}

func TestASignalNobodySentDoesNotExist(t *testing.T) {
	coordinator, _ := coordinators(t)
	if exists, err := coordinator.Exists(context.Background(), "never"); err != nil || exists {
		t.Errorf("exists = %v, err = %v", exists, err)
	}
}

func TestClosingACoordinatorStopsItsListener(t *testing.T) {
	// One of these is built per configured coordinate block, and each starts a
	// goroutine subscribed to Redis. One built per message and never closed is
	// how this leaked goroutines in a live consumer.
	_, client := redisServer(t)

	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		coordinator := NewRedisCoordinatorFromClient(client, "mycel:coord:")
		if err := coordinator.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	// Give the listeners a moment to notice.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > before+5 {
		time.Sleep(50 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+5 {
		t.Errorf("goroutines went from %d to %d after twenty coordinators were opened and closed",
			before, after)
	}
}

func TestClosingTwiceIsNotAFailure(t *testing.T) {
	// Shutdown paths overlap, and the second close must not panic on a channel
	// that is already closed.
	_, client := redisServer(t)
	coordinator := NewRedisCoordinatorFromClient(client, "mycel:coord:")

	if err := coordinator.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := coordinator.Close(); err != nil {
		t.Errorf("closing twice reported a failure: %v", err)
	}
}

func TestClosingAPrimitiveLeavesTheSharedClientAlone(t *testing.T) {
	// The sync manager hands one Redis client to every primitive configured on
	// the same address. Closing one of them used to close that client, so a
	// coordinator going away took the locks and the sequence guard with it —
	// the service carried on running and everything afterwards answered
	// "redis: client is closed".
	_, client := redisServer(t)
	ctx := context.Background()

	coordinator := NewRedisCoordinatorFromClient(client, "mycel:coord:")
	lock := NewRedisLockFromClient(client, "mycel:lock:")

	if err := coordinator.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if acquired, err := lock.Acquire(ctx, "order-1", time.Minute); err != nil || !acquired {
		t.Errorf("the lock stopped working when the coordinator was closed: %v, %v", acquired, err)
	}
	if err := lock.Close(); err != nil {
		t.Errorf("closing a lock that borrowed its client reported a failure: %v", err)
	}
	// And the client is still usable by whoever owns it.
	if err := client.Ping(ctx).Err(); err != nil {
		t.Errorf("the shared client was closed by a borrower: %v", err)
	}
}

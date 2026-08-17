package kafka

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// Closing a Kafka connector cancels its workers and waits a bounded time for
// them — a worker blocked on a broker that has stopped answering must not hold
// the process open. When that bound runs out, Close releases the reader while
// a worker may still be in flight, and the worker dereferenced it:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	kafka-go.(*Reader).activateReadLag(0x0?)
//	mycel/internal/connector/mq/kafka.(*Connector).consumeLoop
//
// A service consuming from Kafka crashed on the way out instead of exiting: a
// crash report on every rolling update, and an exit code of 2 for a service
// that was asked to stop.

func TestAWorkerWithNoReaderStopsInsteadOfCrashing(t *testing.T) {
	c := &Connector{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	c.wg.Add(1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.consumeLoop(context.Background(), 0)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never stopped")
	}
}

func TestAWorkerDoesNotReadTheConnectorsReaderAfterItIsReleased(t *testing.T) {
	// The property behind the fix: the worker takes its reader once, so
	// whatever Close does to the connector's field afterwards cannot be
	// dereferenced by a worker that is still finishing.
	c := &Connector{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	c.wg.Add(1)
	go func() {
		defer wg.Done()
		c.consumeLoop(context.Background(), 0)
	}()

	// Release it from underneath, the way Close does once its bound runs out.
	c.mu.Lock()
	c.reader = nil
	c.mu.Unlock()

	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never stopped")
	}
}

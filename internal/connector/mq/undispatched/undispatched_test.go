package undispatched

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func newLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func rabbitEvent() Event {
	return Event{
		Connector:   "orders_mq",
		Driver:      "rabbitmq",
		Target:      "orders.in.q",
		Key:         "gallery-assets",
		Patterns:    []string{"product.created", "product.updated"},
		Consequence: "nacked without requeue",
	}
}

// A message no flow can handle is dropped. That has to be reported at error
// level: it is a misconfiguration with data loss attached, not a routine event.
func TestReport_FirstOccurrenceLogsError(t *testing.T) {
	var buf bytes.Buffer
	var r Reporter

	r.Report(newLogger(&buf), rabbitEvent())

	out := buf.String()
	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("expected an ERROR log, got: %s", out)
	}
	// The dropped key and the patterns actually registered must both appear —
	// the difference between them is the whole diagnosis.
	for _, want := range []string{"gallery-assets", "product.updated", "orders.in.q", "orders_mq", "nacked"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
}

// A misconfigured consumer drops every message with that key, so logging each
// one would bury the rest of the log while adding nothing.
func TestReport_RepeatsAreSilent(t *testing.T) {
	var buf bytes.Buffer
	var r Reporter
	logger := newLogger(&buf)

	for i := 0; i < 5; i++ {
		r.Report(logger, rabbitEvent())
	}

	if got := strings.Count(buf.String(), "level=ERROR"); got != 1 {
		t.Errorf("expected exactly 1 ERROR log across 5 drops, got %d: %s", got, buf.String())
	}
}

// De-duplication is per key: a second bad key is its own diagnosis.
func TestReport_DistinctKeysEachReport(t *testing.T) {
	var buf bytes.Buffer
	var r Reporter
	logger := newLogger(&buf)

	ev := rabbitEvent()
	r.Report(logger, ev)
	r.Report(logger, ev)
	ev.Key = "style-assets"
	r.Report(logger, ev)

	if got := strings.Count(buf.String(), "level=ERROR"); got != 2 {
		t.Errorf("expected 2 ERROR logs (one per distinct key), got %d: %s", got, buf.String())
	}
}

// Each driver states what it just did with the message, because the outcome
// differs: RabbitMQ may still dead-letter, Kafka commits the offset, Redis
// pub/sub has nothing to retain.
func TestReport_ConsequenceIsLogged(t *testing.T) {
	cases := []struct {
		driver      string
		consequence string
	}{
		{"kafka", "offset committed; the message will not be redelivered"},
		{"redis", "discarded; Redis pub/sub does not retain or redeliver messages"},
	}

	for _, tc := range cases {
		t.Run(tc.driver, func(t *testing.T) {
			var buf bytes.Buffer
			var r Reporter

			r.Report(newLogger(&buf), Event{
				Connector:   "events",
				Driver:      tc.driver,
				Target:      "orders",
				Key:         "orders",
				Consequence: tc.consequence,
			})

			out := buf.String()
			if !strings.Contains(out, tc.driver) {
				t.Errorf("log missing driver %q: %s", tc.driver, out)
			}
			if !strings.Contains(out, "offset committed") && !strings.Contains(out, "does not retain") {
				t.Errorf("log missing consequence: %s", out)
			}
		})
	}
}

// Consumers run one worker per concurrency setting, so Report is called from
// several goroutines at once.
func TestReport_ConcurrentIsSafeAndStillDeduplicates(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	var r Reporter
	// slog handlers are not safe for concurrent writes to the same buffer;
	// serialize the sink, not the Reporter under test.
	logger := slog.New(slog.NewTextHandler(&syncWriter{w: &buf, mu: &mu}, nil))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Report(logger, rabbitEvent())
		}()
	}
	wg.Wait()

	if got := strings.Count(buf.String(), "level=ERROR"); got != 1 {
		t.Errorf("expected exactly 1 ERROR log across 50 concurrent drops, got %d", got)
	}
}

type syncWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func TestReport_NilLoggerDoesNotPanic(t *testing.T) {
	var r Reporter
	r.Report(nil, rabbitEvent())
}

func TestSortedPatterns(t *testing.T) {
	handlers := map[string]int{"product.updated": 1, "asset.created": 2, "order.placed": 3}

	got := SortedPatterns(handlers)
	want := []string{"asset.created", "order.placed", "product.updated"}
	if len(got) != len(want) {
		t.Fatalf("expected %d patterns, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pattern %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

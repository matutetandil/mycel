package sync

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	gosync "sync"
	"testing"
	"time"
)

func TestMemorySequenceGuard_ReadEmpty(t *testing.T) {
	g := NewMemorySequenceGuard(time.Hour)
	defer g.Close()

	seq, ok, err := g.Read(context.Background(), "sku:AI02LT")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false on missing key, got seq=%d", seq)
	}
}

func TestMemorySequenceGuard_WriteThenRead(t *testing.T) {
	g := NewMemorySequenceGuard(time.Hour)
	defer g.Close()

	if err := g.Write(context.Background(), "sku:AI02LT", 42, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}

	seq, ok, err := g.Read(context.Background(), "sku:AI02LT")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok || seq != 42 {
		t.Errorf("expected seq=42 ok=true, got seq=%d ok=%v", seq, ok)
	}
}

func TestMemorySequenceGuard_TTLExpiry(t *testing.T) {
	g := NewMemorySequenceGuard(time.Hour) // long reaper, expiry on read still works
	defer g.Close()

	if err := g.Write(context.Background(), "sku:X", 1, 10*time.Millisecond); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Immediately readable.
	if _, ok, _ := g.Read(context.Background(), "sku:X"); !ok {
		t.Fatal("expected immediate read to succeed")
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok, _ := g.Read(context.Background(), "sku:X"); ok {
		t.Error("expected expired key to read as missing")
	}
}

func TestMemorySequenceGuard_DistinctKeys(t *testing.T) {
	g := NewMemorySequenceGuard(time.Hour)
	defer g.Close()

	_ = g.Write(context.Background(), "a", 1, 0)
	_ = g.Write(context.Background(), "b", 2, 0)

	if seq, _, _ := g.Read(context.Background(), "a"); seq != 1 {
		t.Errorf("a: got %d", seq)
	}
	if seq, _, _ := g.Read(context.Background(), "b"); seq != 2 {
		t.Errorf("b: got %d", seq)
	}
}

func TestMemorySequenceGuard_OverwriteIsAllowed(t *testing.T) {
	g := NewMemorySequenceGuard(time.Hour)
	defer g.Close()

	_ = g.Write(context.Background(), "k", 5, 0)
	_ = g.Write(context.Background(), "k", 10, 0)

	seq, _, _ := g.Read(context.Background(), "k")
	if seq != 10 {
		t.Errorf("expected 10, got %d", seq)
	}
}

// TestExecuteWithSequenceGuard_LogsRejection guards the regression where
// a sequence_guard rejection produced no log line — operators saw a
// suspiciously fast "request" entry and the rest of the flow body
// silently skipped, with no signal as to which gate had short-circuited.
// The fix logs at INFO with key, stored, and current so the disposition
// is visible at the default log level.
func TestExecuteWithSequenceGuard_LogsRejection(t *testing.T) {
	var buf logCapture
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(original)

	mgr := NewManager()
	defer mgr.Close()

	storage := &SyncStorageConfig{Driver: "memory"}
	cfg := &FlowSequenceGuardConfig{Storage: storage}

	// Seed the store with a high value, then attempt a lower one.
	guard, _ := mgr.GetSequenceGuard(context.Background(), storage)
	_ = guard.Write(context.Background(), "k", 100, 0)

	called := false
	_, err := mgr.ExecuteWithSequenceGuard(context.Background(), cfg, "k", 50, func() (interface{}, error) {
		called = true
		return "should not run", nil
	})
	if !errors.As(err, new(*SequenceGuardSkippedError)) {
		t.Fatalf("expected SequenceGuardSkippedError, got %v", err)
	}
	if called {
		t.Error("flow body must not run when sequence is older")
	}

	out := buf.String()
	if !strings.Contains(out, "sequence guard skipped") {
		t.Errorf("expected INFO log line on rejection, got: %s", out)
	}
	if !strings.Contains(out, "stored=100") || !strings.Contains(out, "current=50") {
		t.Errorf("rejection log should include stored and current; got: %s", out)
	}
}

// TestExecuteWithSequenceGuard_LogsPass: the success/init paths also
// emit INFO so operators can confirm the guard ran (vs silently bypassed
// because of a config issue).
func TestExecuteWithSequenceGuard_LogsPass(t *testing.T) {
	var buf logCapture
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(original)

	mgr := NewManager()
	defer mgr.Close()
	storage := &SyncStorageConfig{Driver: "memory"}
	cfg := &FlowSequenceGuardConfig{Storage: storage}

	// First call — no stored value; expect "initialized" log.
	_, err := mgr.ExecuteWithSequenceGuard(context.Background(), cfg, "k2", 1, func() (interface{}, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call with a higher sequence — expect "passed" log.
	_, err = mgr.ExecuteWithSequenceGuard(context.Background(), cfg, "k2", 2, func() (interface{}, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "sequence guard initialized") {
		t.Errorf("expected initialized log on first call, got: %s", out)
	}
	if !strings.Contains(out, "sequence guard passed") {
		t.Errorf("expected passed log on second call, got: %s", out)
	}
}

// logCapture is a thread-safe write target for slog tests.
type logCapture struct {
	mu  gosync.Mutex
	buf []byte
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	return len(p), nil
}

func (l *logCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}

func TestParseOnOlder(t *testing.T) {
	tests := map[string]SequenceGuardOnOlder{
		"ack":     OnOlderAck,
		"reject":  OnOlderReject,
		"requeue": OnOlderRequeue,
		"":        OnOlderAck,
		"garbage": OnOlderAck,
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := ParseOnOlder(in); got != want {
				t.Errorf("ParseOnOlder(%q) = %v, want %v", in, got, want)
			}
		})
	}
}

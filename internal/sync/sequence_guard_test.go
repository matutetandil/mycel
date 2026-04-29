package sync

import (
	"context"
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

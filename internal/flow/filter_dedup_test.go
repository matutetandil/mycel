package flow

import (
	"testing"
	"time"
)

func TestRequeueTracker_IncrementAndCheck(t *testing.T) {
	tracker := NewRequeueTracker(10 * time.Minute)
	defer tracker.Stop()

	// First increment should not reach max
	count, shouldAck := tracker.IncrementAndCheck("msg-1", 3)
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
	if shouldAck {
		t.Error("should not ACK after first attempt")
	}

	// Second increment
	count, shouldAck = tracker.IncrementAndCheck("msg-1", 3)
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
	if shouldAck {
		t.Error("should not ACK after second attempt")
	}

	// Third increment should reach max
	count, shouldAck = tracker.IncrementAndCheck("msg-1", 3)
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
	if !shouldAck {
		t.Error("should ACK after reaching max requeue")
	}
}

func TestRequeueTracker_IndependentMessages(t *testing.T) {
	tracker := NewRequeueTracker(10 * time.Minute)
	defer tracker.Stop()

	tracker.IncrementAndCheck("msg-a", 3)
	tracker.IncrementAndCheck("msg-a", 3)

	count, _ := tracker.IncrementAndCheck("msg-b", 3)
	if count != 1 {
		t.Errorf("msg-b should have count 1, got %d", count)
	}

	if tracker.Count("msg-a") != 2 {
		t.Errorf("msg-a should have count 2, got %d", tracker.Count("msg-a"))
	}
}

func TestRequeueTracker_DefaultMaxRequeue(t *testing.T) {
	tracker := NewRequeueTracker(10 * time.Minute)
	defer tracker.Stop()

	// max_requeue=0 should default to 3
	for i := 0; i < 2; i++ {
		_, shouldAck := tracker.IncrementAndCheck("msg-1", 0)
		if shouldAck {
			t.Errorf("should not ACK on attempt %d with default max", i+1)
		}
	}
	_, shouldAck := tracker.IncrementAndCheck("msg-1", 0)
	if !shouldAck {
		t.Error("should ACK on attempt 3 with default max")
	}
}

func TestRequeueTracker_Cleanup(t *testing.T) {
	tracker := NewRequeueTracker(50 * time.Millisecond)
	defer tracker.Stop()

	tracker.IncrementAndCheck("msg-1", 5)
	if tracker.Count("msg-1") != 1 {
		t.Errorf("expected count 1, got %d", tracker.Count("msg-1"))
	}

	// Wait for entry to expire
	time.Sleep(100 * time.Millisecond)
	tracker.cleanup()

	if tracker.Count("msg-1") != 0 {
		t.Errorf("expected count 0 after cleanup, got %d", tracker.Count("msg-1"))
	}
}

func TestRequeueTracker_MaxRequeueOne(t *testing.T) {
	tracker := NewRequeueTracker(10 * time.Minute)
	defer tracker.Stop()

	// With max_requeue=1, first attempt should ACK
	count, shouldAck := tracker.IncrementAndCheck("msg-1", 1)
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
	if !shouldAck {
		t.Error("should ACK immediately when max_requeue=1")
	}
}

func TestRequeueTracker_Stop(t *testing.T) {
	tracker := NewRequeueTracker(10 * time.Minute)
	tracker.Stop()
	// Double stop should not panic
	tracker.Stop()
}

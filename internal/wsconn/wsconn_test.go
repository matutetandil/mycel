package wsconn

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestIdleExpiresWhenNothingTouchesIt(t *testing.T) {
	var fired atomic.Bool
	idle := NewIdle(30*time.Millisecond, func() { fired.Store(true) })
	defer idle.Stop()

	time.Sleep(80 * time.Millisecond)
	if !fired.Load() {
		t.Fatal("an idle connection was never expired")
	}
}

// The point of the timer: a connection that keeps showing signs of life is
// never dropped, however long it lives.
func TestIdleTouchKeepsItAlive(t *testing.T) {
	var fired atomic.Bool
	idle := NewIdle(60*time.Millisecond, func() { fired.Store(true) })
	defer idle.Stop()

	for range 8 {
		time.Sleep(20 * time.Millisecond)
		idle.Touch()
	}
	if fired.Load() {
		t.Fatal("a connection touched every 20ms expired on a 60ms timeout")
	}
}

func TestIdleStopPreventsExpiry(t *testing.T) {
	var fired atomic.Bool
	idle := NewIdle(30*time.Millisecond, func() { fired.Store(true) })
	idle.Stop()
	idle.Touch() // must not re-arm a stopped timer

	time.Sleep(80 * time.Millisecond)
	if fired.Load() {
		t.Fatal("a stopped timer expired")
	}
}

func TestIdleWithoutTimeoutIsANoOp(t *testing.T) {
	idle := NewIdle(0, func() { t.Fatal("expired without a timeout") })
	if idle != nil {
		t.Fatal("NewIdle(0) should return nil")
	}
	idle.Touch()
	idle.Stop()
}

func TestUnexpectedClose(t *testing.T) {
	normal := fmt.Errorf("read: %w", websocket.CloseError{Code: websocket.StatusNormalClosure})
	policy := fmt.Errorf("read: %w", websocket.CloseError{Code: websocket.StatusPolicyViolation})

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"expected status", normal, false},
		{"status not in the list", policy, true},
		{"not a close frame", errors.New("connection reset by peer"), false},
	}
	for _, tc := range cases {
		if got := UnexpectedClose(tc.err, websocket.StatusNormalClosure, websocket.StatusGoingAway); got != tc.want {
			t.Errorf("%s: UnexpectedClose = %v, want %v", tc.name, got, tc.want)
		}
	}
}

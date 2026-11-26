// Package wsconn holds what Mycel's WebSocket servers share on top of
// github.com/coder/websocket.
//
// The library has no read deadline to push forward: a read is bounded by a
// context, and a context's deadline cannot be moved. Mycel's servers drop a
// connection that has gone quiet — no message and no answer to a ping — for
// longer than a timeout, so the timeout lives here as a timer that every sign
// of life resets.
package wsconn

import (
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Idle runs expire once nothing has touched it for the timeout. A nil *Idle,
// which NewIdle returns for a timeout of zero or less, does nothing, so a
// connection without a timeout needs no special case.
type Idle struct {
	mu      sync.Mutex
	timer   *time.Timer
	timeout time.Duration
	stopped bool
}

// NewIdle starts the timer.
func NewIdle(timeout time.Duration, expire func()) *Idle {
	if timeout <= 0 {
		return nil
	}
	return &Idle{timeout: timeout, timer: time.AfterFunc(timeout, expire)}
}

// Touch records a sign of life: the timeout starts over.
func (i *Idle) Touch() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.stopped {
		i.timer.Reset(i.timeout)
	}
}

// Stop ends the timer without running expire. Touch after Stop does nothing.
func (i *Idle) Stop() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.stopped = true
	i.timer.Stop()
}

// UnexpectedClose reports whether a read error is the peer closing with a
// status other than the ones given. Any other error — a dropped socket, a
// timeout, a connection this side closed — is not a close frame and reports
// false, so only a peer that hung up for a reason worth reading gets logged.
func UnexpectedClose(err error, expected ...websocket.StatusCode) bool {
	var ce websocket.CloseError
	if !errors.As(err, &ce) {
		return false
	}
	for _, code := range expected {
		if ce.Code == code {
			return false
		}
	}
	return true
}

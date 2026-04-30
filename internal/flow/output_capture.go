package flow

import (
	"context"
	gosync "sync"
)

// outputCaptureCtxKey is used to attach a write-once slot for the
// transform output to a flow's context. Wrappers (coordinate.signal,
// future hooks) read it via TransformOutputFromContext to get the
// post-transform payload — this is what users mean when they reference
// `output.*` in coordinate.signal.emit, not the destination's response.
type outputCaptureCtxKey struct{}

// OutputSlot is a goroutine-safe holder for the transform output of one
// flow execution. Wrappers create it, attach it to context with
// WithOutputCapture, and the runtime sets it via SetTransformOutput right
// after applyTransforms succeeds.
type OutputSlot struct {
	mu  gosync.RWMutex
	val map[string]interface{}
}

// Set stores the transform output. Safe to call once per flow execution.
func (s *OutputSlot) Set(out map[string]interface{}) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.val = out
}

// Get returns the captured output (may be nil if no transform ran or the
// flow failed before the transform stage).
func (s *OutputSlot) Get() map[string]interface{} {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.val
}

// WithOutputCapture attaches a fresh OutputSlot to ctx. Use at the entry
// of executeFlowCore so any wrapper or downstream consumer can read the
// transform output later in the lifecycle.
func WithOutputCapture(ctx context.Context, slot *OutputSlot) context.Context {
	return context.WithValue(ctx, outputCaptureCtxKey{}, slot)
}

// TransformOutputFromContext returns the slot attached to ctx, or nil if
// none was attached.
func TransformOutputFromContext(ctx context.Context) *OutputSlot {
	slot, _ := ctx.Value(outputCaptureCtxKey{}).(*OutputSlot)
	return slot
}

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
)

// newEHHandler builds a minimal FlowHandler carrying only an error_handling
// config — enough to exercise executeWithRetry's class-handler routing without
// a real source/destination.
func newEHHandler(eh *flow.ErrorHandlingConfig) *FlowHandler {
	return &FlowHandler{Config: &flow.Config{ErrorHandling: eh}}
}

// TestOnTimeoutAck: the driving case. A timeout with on_timeout{action="ack"}
// must NOT consume the retry budget and must surface an ACK disposition so the
// consumer drops the message instead of replaying it into a concurrent
// duplicate on the remote side.
func TestOnTimeoutAckNoRetryAndAcks(t *testing.T) {
	h := newEHHandler(&flow.ErrorHandlingConfig{
		Retry:     &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
		OnTimeout: &flow.ErrorClassHandler{Action: "ack"},
	})

	calls := 0
	_, err := h.executeWithRetry(context.Background(), nil, func() (interface{}, error) {
		calls++
		return nil, context.DeadlineExceeded
	})

	if calls != 1 {
		t.Fatalf("expected exactly 1 call (no retry on ack), got %d", calls)
	}
	disp, ok := connector.GetDisposition(err)
	if !ok || disp != connector.DispositionAck {
		t.Fatalf("expected ack disposition, got disp=%q ok=%v err=%v", disp, ok, err)
	}
	// ack must read as permanent so even a consumer that only knows
	// permanent-vs-transient still drops it.
	if !connector.IsPermanent(err) {
		t.Error("ack disposition must be permanent for IsPermanent-only consumers")
	}
}

// TestTimeoutWithoutHandlerRetriesAndRequeues: backward compatibility. Without
// on_timeout, a timeout stays transient — full retry budget, then a plain
// (non-permanent, no-disposition) error so the consumer requeues. This is the
// pre-existing behavior and must not change.
func TestTimeoutWithoutHandlerRetriesAndRequeues(t *testing.T) {
	h := newEHHandler(&flow.ErrorHandlingConfig{
		Retry: &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
	})

	calls := 0
	_, err := h.executeWithRetry(context.Background(), nil, func() (interface{}, error) {
		calls++
		return nil, context.DeadlineExceeded
	})

	if calls != 3 {
		t.Fatalf("expected 3 calls (full retry budget), got %d", calls)
	}
	if disp, ok := connector.GetDisposition(err); ok {
		t.Errorf("expected no explicit disposition (default requeue), got %q", disp)
	}
	if connector.IsPermanent(err) {
		t.Error("timeout without handler must stay transient (requeue), not permanent")
	}
}

// TestOnErrorRequeue: a generic transient error with on_error{action="requeue"}
// surfaces a requeue disposition and short-circuits (no retry).
func TestOnErrorRequeue(t *testing.T) {
	h := newEHHandler(&flow.ErrorHandlingConfig{
		Retry:   &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
		OnError: &flow.ErrorClassHandler{Action: "requeue"},
	})

	calls := 0
	_, err := h.executeWithRetry(context.Background(), nil, func() (interface{}, error) {
		calls++
		return nil, errors.New("downstream blew up")
	})

	if calls != 1 {
		t.Fatalf("expected 1 call (requeue short-circuits), got %d", calls)
	}
	disp, ok := connector.GetDisposition(err)
	if !ok || disp != connector.DispositionRequeue {
		t.Fatalf("expected requeue disposition, got disp=%q ok=%v", disp, ok)
	}
	if connector.IsPermanent(err) {
		t.Error("requeue disposition must not be permanent")
	}
}

// TestOnTimeoutRetryUsesBudget: on_timeout{action="retry"} explicitly opts the
// timeout class into the retry budget (falls through), matching the default.
func TestOnTimeoutRetryUsesBudget(t *testing.T) {
	h := newEHHandler(&flow.ErrorHandlingConfig{
		Retry:     &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
		OnTimeout: &flow.ErrorClassHandler{Action: "retry"},
	})

	calls := 0
	_, err := h.executeWithRetry(context.Background(), nil, func() (interface{}, error) {
		calls++
		return nil, context.DeadlineExceeded
	})

	if calls != 3 {
		t.Fatalf("expected 3 calls (retry budget), got %d", calls)
	}
	if disp, ok := connector.GetDisposition(err); ok {
		t.Errorf("retry must not attach a disposition after exhausting the budget, got %q", disp)
	}
}

// TestOnErrorSkipsPermanent: on_error must NOT capture permanent errors (HTTP
// 4xx). They break the retry loop early and keep their ack-and-drop behavior.
func TestOnErrorSkipsPermanent(t *testing.T) {
	h := newEHHandler(&flow.ErrorHandlingConfig{
		Retry:   &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
		OnError: &flow.ErrorClassHandler{Action: "requeue"},
	})

	calls := 0
	_, err := h.executeWithRetry(context.Background(), nil, func() (interface{}, error) {
		calls++
		return nil, &httpconn.HTTPError{StatusCode: 409}
	})

	if calls != 1 {
		t.Fatalf("expected 1 call (permanent breaks early), got %d", calls)
	}
	if disp, ok := connector.GetDisposition(err); ok {
		t.Errorf("permanent error must not get an on_error disposition, got %q", disp)
	}
	if !connector.IsPermanent(err) {
		t.Error("permanent error must stay permanent (ack-drop)")
	}
}

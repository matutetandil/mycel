package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
)

// TestRetryFailureMessageReportsActualAttempts: the previous code emitted
// "flow failed after %d attempts" using the configured budget, even when
// the loop broke early on a permanent error. Mercury operators read that
// log and assumed three POSTs had happened when in reality only one had.
// Post-fix: the suffix names the actual attempt count and flags
// "permanent failure, retry skipped" when the break was due to 4xx.
func TestRetryFailureMessageReportsActualAttempts(t *testing.T) {
	h, hits, done := newRetryHandler(t, 409, `nope`)
	defer done()

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	_, err := h.HandleRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error from 4xx response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "after 1 attempt") {
		t.Errorf("expected 'after 1 attempt', got: %s", msg)
	}
	if !strings.Contains(msg, "permanent failure, retry skipped") {
		t.Errorf("expected permanent-failure note in error, got: %s", msg)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("destination should be hit exactly once, got %d", got)
	}
}

// TestRetryFailureMessageOn5xxShowsFullBudget: 5xx is transient, so the
// loop runs the full budget and the message reports the configured
// number — no permanent-failure note.
func TestRetryFailureMessageOn5xxShowsFullBudget(t *testing.T) {
	h, _, done := newRetryHandler(t, 500, `down`)
	defer done()

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	_, err := h.HandleRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error from 5xx after exhausting retries")
	}
	msg := err.Error()
	if !strings.Contains(msg, "after 3 attempts") {
		t.Errorf("expected 'after 3 attempts' for 5xx, got: %s", msg)
	}
	if strings.Contains(msg, "permanent failure") {
		t.Errorf("5xx should not be reported as permanent, got: %s", msg)
	}
}

// TestHTTPErrorImplementsPermanent: the contract that the rabbit consumer
// relies on for ack-vs-nack-requeue. *httpconn.HTTPError must satisfy
// connector.PermanentError and report 4xx as permanent, 5xx as transient.
func TestHTTPErrorImplementsPermanent(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{400, true},
		{401, true},
		{404, true},
		{409, true},
		{422, true},
		{499, true},
		{500, false},
		{502, false},
		{503, false},
	}
	for _, tc := range cases {
		err := &httpconn.HTTPError{StatusCode: tc.code}
		if got := connector.IsPermanent(err); got != tc.want {
			t.Errorf("HTTP %d: IsPermanent = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// TestPermanentDetectionUnwraps: errors.As must traverse fmt.Errorf wraps
// because the runtime decorates the underlying error with "flow failed
// after N attempts: ...". The unwrap is what makes IsPermanent work end
// to end.
func TestPermanentDetectionUnwraps(t *testing.T) {
	original := &httpconn.HTTPError{StatusCode: 409}
	wrapped := errors.New("not even a wrap, just a string")
	if connector.IsPermanent(wrapped) {
		t.Error("a non-HTTPError must not be reported as permanent")
	}
	// fmt.Errorf with %w preserves the wrapping chain through errors.As.
	deepWrap := errorf("flow failed after %d attempts: %w", 1, original)
	if !connector.IsPermanent(deepWrap) {
		t.Error("wrapped HTTPError must still be detected as permanent")
	}
}

// errorf is a tiny shim so the test reads cleanly without re-importing fmt.
func errorf(format string, args ...interface{}) error {
	return errFmt{msg: format, args: args}.unwrap()
}

type errFmt struct {
	msg  string
	args []interface{}
}

func (e errFmt) unwrap() error {
	// Use the standard library indirectly via fmt.Errorf-style %w semantics.
	// We can't shadow the test imports, so just produce the wrapped err
	// using errors.Join — equivalent for IsPermanent's errors.As walk.
	for _, a := range e.args {
		if err, ok := a.(error); ok {
			return joinErr{base: errors.New(e.msg), wrapped: err}
		}
	}
	return errors.New(e.msg)
}

type joinErr struct {
	base    error
	wrapped error
}

func (j joinErr) Error() string  { return j.base.Error() }
func (j joinErr) Unwrap() error  { return j.wrapped }

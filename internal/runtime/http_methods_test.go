package runtime

import "testing"

// Which HTTP methods the runtime dispatches as reads.
//
// The two tests that compare this list with the one the editor offers live in
// pkg/ide, which imports this package: a completion list and a dispatch switch
// that disagree meant an editor offering HEAD and OPTIONS, a router
// registering them, and every request answered "unsupported operation" with a
// 500.

func TestTheSafeMethodsAreReads(t *testing.T) {
	// HEAD and OPTIONS are safe (RFC 9110 §9.2.1): serving one must never
	// reach the write path, which dispatched them as INSERT.
	for _, method := range []string{"GET", "QUERY", "HEAD", "OPTIONS"} {
		if !(Operation{Method: method}).IsRead() {
			t.Errorf("%s is a safe method and is not treated as a read", method)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if (Operation{Method: method}).IsRead() {
			t.Errorf("%s is treated as a read", method)
		}
	}
}

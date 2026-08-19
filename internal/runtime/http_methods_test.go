package runtime

import (
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/ide"
)

// What the editor offers, the runtime has to be able to serve.
//
// The completion list and the dispatch switch were two separate lists, and they
// disagreed: an editor offered HEAD and OPTIONS for a flow's operation, the
// router registered them, the banner printed them, and every request to one was
// answered "unsupported operation" with a 500. The method was accepted
// everywhere except at the point where it had to do something.

func TestEveryMethodTheEditorOffersCanBeDispatched(t *testing.T) {
	offered := ide.HTTPMethods()
	if len(offered) == 0 {
		t.Fatal("the editor offers no HTTP methods; this test is checking nothing")
	}

	// The write methods the dispatch switch names, kept next to the switch it
	// mirrors rather than derived from it — a test that reads the same list as
	// the code proves only that the list exists.
	writes := map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

	for _, method := range offered {
		op := Operation{Method: method}
		if op.IsRead() || writes[method] {
			continue
		}
		t.Errorf("the editor offers %s for a flow's operation and the runtime dispatches "+
			"neither a read nor a write for it, so a flow written that way answers 500", method)
	}
}

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

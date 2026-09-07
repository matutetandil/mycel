package ide

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/runtime"
)

// What the editor offers, the runtime has to be able to serve.
//
// The completion list and the dispatch switch were two separate lists, and they
// disagreed: an editor offered HEAD and OPTIONS for a flow's operation, the
// router registered them, the banner printed them, and every request to one was
// answered "unsupported operation" with a 500. The method was accepted
// everywhere except at the point where it had to do something.
//
// The pair lives here rather than beside the runtime because the editor now
// runs the runtime's checks, and a package cannot be imported by something it
// imports, tests included.

func TestEveryMethodTheEditorOffersCanBeDispatched(t *testing.T) {
	offered := HTTPMethods()
	if len(offered) == 0 {
		t.Fatal("the editor offers no HTTP methods; this test is checking nothing")
	}

	// The write methods the dispatch switch names, kept next to the switch it
	// mirrors rather than derived from it — a test that reads the same list as
	// the code proves only that the list exists.
	writes := map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

	for _, method := range offered {
		op := runtime.Operation{Method: method}
		if op.IsRead() || writes[method] {
			continue
		}
		t.Errorf("the editor offers %s for a flow's operation and the runtime dispatches "+
			"neither a read nor a write for it, so a flow written that way answers 500", method)
	}
}

func TestTheEditorAndTheRuntimeAgreeOnWhatReads(t *testing.T) {
	// The editor names the destination stage from this answer — "read" or
	// "write" — and the runtime records the stage it actually reaches. Two
	// answers means a breakpoint offered at one and reached at the other.
	for _, method := range HTTPMethods() {
		if IsReadMethod(method) != (runtime.Operation{Method: method}).IsRead() {
			t.Errorf("the editor says %s reads: %v; the runtime says %v",
				method, IsReadMethod(method), (runtime.Operation{Method: method}).IsRead())
		}
	}
}

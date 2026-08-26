package transform

import (
	"context"
	"encoding/json"
	"testing"
)

// These are the CEL entry points the runtime actually calls and that had no
// coverage: TransformResponse behind a flow's `response` block,
// EvaluateExpressionWithOutput behind dedupe fingerprints, and
// EvaluateExpressionWithSteps behind step conditions. They differ only in
// which variables they bind, and binding the wrong set is a silent failure —
// the expression evaluates against a missing key and the flow carries on with
// an empty value.

func newT(t *testing.T) *CELTransformer {
	t.Helper()
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return tr
}

func TestTransformResponseBindsInputAndOutput(t *testing.T) {
	tr := newT(t)
	ctx := context.Background()

	// In a response block `input` is the original request and `output` is what
	// the destination returned. Swapping them would quietly produce a response
	// shaped from the wrong data.
	in := map[string]interface{}{"requested_by": "alice"}
	out := map[string]interface{}{"id": "row-1", "total": 42.0}

	got, err := tr.TransformResponse(ctx, in, out, RulesFromMappings(map[string]string{
		"order_id":  "output.id",
		"total":     "output.total",
		"requester": "input.requested_by",
		"status":    "'created'",
	}, []string{"order_id", "total", "requester", "status"}))
	if err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}

	for k, want := range map[string]interface{}{
		"order_id":  "row-1",
		"total":     42.0,
		"requester": "alice",
		"status":    "created",
	} {
		if got[k] != want {
			t.Errorf("%s = %#v, want %#v", k, got[k], want)
		}
	}
}

func TestTransformResponseFailsLoudlyOnABadExpression(t *testing.T) {
	// A response mapping that cannot compile must be an error, not a field
	// silently missing from what the caller gets back.
	tr := newT(t)
	_, err := tr.TransformResponse(context.Background(),
		map[string]interface{}{}, map[string]interface{}{},
		[]Rule{{Target: "x", Expression: "this is not ( valid CEL"}})
	if err == nil {
		t.Error("an uncompilable response mapping was accepted")
	}
}

func TestEvaluateExpressionWithOutput(t *testing.T) {
	// This is what dedupe fingerprints run on: both input and the transformed
	// payload have to be reachable, because a fingerprint usually mixes them.
	tr := newT(t)
	ctx := context.Background()

	in := map[string]interface{}{"message_id": "m-1"}
	out := map[string]interface{}{"sku": "ABC", "qty": 3.0}

	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"input.message_id", "m-1"},
		{"output.sku", "ABC"},
		{"output.sku + '/' + input.message_id", "ABC/m-1"},
	} {
		got, err := tr.EvaluateExpressionWithOutput(ctx, in, out, tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %#v, want %#v", tc.expr, got, tc.want)
		}
	}
}

func TestEvaluateExpressionWithOutputToleratesANilOutput(t *testing.T) {
	// The fingerprint runs before the payload exists in some paths; a nil
	// output must leave `output` bound to something rather than panicking.
	tr := newT(t)
	got, err := tr.EvaluateExpressionWithOutput(context.Background(),
		map[string]interface{}{"a": "1"}, nil, "input.a")
	if err != nil {
		t.Fatalf("with a nil output: %v", err)
	}
	if got != "1" {
		t.Errorf("got %#v, want 1", got)
	}
}

func TestEvaluateExpressionWithSteps(t *testing.T) {
	// Step conditions and later steps read earlier ones as step.<name>.<field>.
	tr := newT(t)
	steps := map[string]interface{}{
		"order":    map[string]interface{}{"id": "o-1", "total": 120.0},
		"customer": map[string]interface{}{"tier": "gold"},
	}

	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"step.order.id", "o-1"},
		{"step.customer.tier == 'gold'", true},
		{"step.order.total > 100.0", true},
		{"input.region", "eu"},
	} {
		got, err := tr.EvaluateExpressionWithSteps(context.Background(),
			map[string]interface{}{"region": "eu"}, steps, tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %#v, want %#v", tc.expr, got, tc.want)
		}
	}
}

func TestEvaluateWithBindsAnArbitraryActivation(t *testing.T) {
	// EvaluateWith is the general form the others are built on; whatever the
	// caller puts in the activation has to be visible under that name.
	tr := newT(t)
	got, err := tr.EvaluateWith(context.Background(), "error.message", map[string]interface{}{
		"error": map[string]interface{}{"message": "boom", "code": 500},
	})
	if err != nil {
		t.Fatalf("EvaluateWith: %v", err)
	}
	if got != "boom" {
		t.Errorf("got %#v, want boom", got)
	}
}

func TestGoTimeFormat(t *testing.T) {
	// format_date takes a human pattern and hands Go a reference-time layout.
	// A token that fails to translate silently formats as itself, so the
	// output looks like a literal "YYYY" in the payload.
	for _, tc := range []struct{ in, want string }{
		{"YYYY-MM-DD", "2006-01-02"},
		{"DD/MM/YYYY", "02/01/2006"},
		{"YYYY-MM-DD HH:mm:ss", "2006-01-02 15:04:05"},
		{"HH:mm", "15:04"},
		{"", ""},
	} {
		if got := goTimeFormat(tc.in); got != tc.want {
			t.Errorf("goTimeFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHashSHA256IsActuallySHA256(t *testing.T) {
	// The function is named hash_sha256 and the documentation offers it for
	// hashing a password. It was neither: it returned a 64-bit djb2 hash, with
	// a comment saying to use crypto/sha256 in production. Anyone who took the
	// name at its word got 16 hex characters of a non-cryptographic hash.
	tr := newT(t)
	out, err := tr.Evaluate(context.Background(), `hash_sha256("abc")`, map[string]interface{}{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// The published SHA-256 of "abc".
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if out != want {
		t.Errorf("hash_sha256(\"abc\") = %v, want %s", out, want)
	}
}

func TestTransformWithStepsCombinesEverything(t *testing.T) {
	// The main flow path: enrichments, step results and the growing output all
	// visible to the same rule list, in order.
	tr := newT(t)
	rules := RulesFromMappings(map[string]string{
		"base":   "input.amount",
		"markup": "output.base * 0.1",
		"total":  "output.base + output.markup",
		"tier":   "enriched.customer.tier",
		"origin": "step.lookup.country",
	}, []string{"base", "markup", "total", "tier", "origin"})

	got, err := tr.TransformWithSteps(context.Background(),
		map[string]interface{}{"amount": 100.0},
		map[string]interface{}{"customer": map[string]interface{}{"tier": "gold"}},
		map[string]interface{}{"lookup": map[string]interface{}{"country": "AR"}},
		rules)
	if err != nil {
		t.Fatalf("TransformWithSteps: %v", err)
	}

	for k, want := range map[string]interface{}{
		"base":   100.0,
		"markup": 10.0,
		"total":  110.0,
		"tier":   "gold",
		"origin": "AR",
	} {
		if got[k] != want {
			t.Errorf("%s = %#v, want %#v", k, got[k], want)
		}
	}
}

func TestResponseTransformUnwrapsStructuredValues(t *testing.T) {
	// The response block ran its rules through a shallow val.Value(), so an
	// expression returning a list of objects — anything built with filter, map
	// or sort_by — handed back CEL's own wrappers. They JSON-encode as
	// {"Adapter":{}}, so a flow shaping its output with a response block
	// answered 200 with the data replaced by an empty struct. Every other rule
	// loop in cel.go already unwrapped; this one was the exception.
	tr := newT(t)
	input := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "a", "price": 10},
			map[string]interface{}{"name": "b", "price": 80},
		},
	}
	rules := RulesFromMappings(map[string]string{
		"expensive": "input.items.filter(x, x.price > 50)",
	}, []string{"expensive"})

	out, err := tr.TransformResponseWithSteps(context.Background(), input, map[string]interface{}{}, nil, rules)
	if err != nil {
		t.Fatalf("transform response: %v", err)
	}
	encoded, err := json.Marshal(out["expensive"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `[{"name":"b","price":80}]`
	if string(encoded) != want {
		t.Errorf("response gave %s, want %s", encoded, want)
	}
}

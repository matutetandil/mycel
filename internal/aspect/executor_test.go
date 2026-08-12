package aspect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
)

// An aspect's condition decides whether cross-cutting work runs at all —
// auditing, notifications, dead-lettering. Both ways of getting it wrong are
// quiet: a condition that wrongly evaluates false means the audit trail simply
// has no entry, and one that wrongly evaluates true fires a notification for
// something that did not happen.

func newExecutor(t *testing.T) *Executor {
	t.Helper()
	e, err := NewExecutor(NewRegistry(), connector.NewRegistry())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return e
}

func TestEvaluateCondition(t *testing.T) {
	e := newExecutor(t)
	ctx := context.Background()

	input := map[string]interface{}{
		"amount":     150.0,
		"_flow":      "create_order",
		"_operation": "POST /orders",
		"_timestamp": int64(1700000000),
	}

	tests := []struct {
		name    string
		cond    string
		result  *connector.Result
		flowErr error
		want    bool
	}{
		{"an empty condition always runs", "", nil, nil, true},

		// `input` is the flow input. It used to be bound one level deeper,
		// so `input.amount` silently evaluated false and the aspect never
		// ran; these two are the regression guard for that.
		{"the flow input is at input.x", "input.amount > 100.0", nil, nil, true},
		{"the flow input, false side", "input.amount > 1000.0", nil, nil, false},
		{"there is no second input level", "has(input.input)", nil, nil, false},

		{"flow metadata is bound at the top level", "_flow == 'create_order'", nil, nil, true},
		{"operation metadata", "_operation == 'POST /orders'", nil, nil, true},

		// The error object must exist even on the success path, or every
		// on_error condition becomes an evaluation error instead of false.
		{"error is bound as an empty object on success", "error.message == ''", nil, nil, true},
		{"error carries the message when there is one", "error.message != ''", nil, errors.New("boom"), true},

		// Same for result: an unbound variable would make the condition error
		// out and be treated as false, which looks identical to "did not match".
		{"result is bound even when nil", "has(result.affected) || true", nil, nil, true},
		{"result reads affected rows", "result.affected > 0", &connector.Result{Affected: 3}, nil, true},
		{"result affected, false side", "result.affected > 10", &connector.Result{Affected: 3}, nil, false},

		// Every variable the CEL environment declares has to be bound, or
		// referencing it is an evaluation error that reads as false and is
		// indistinguishable from "did not match".
		{"step is bound rather than missing", "!has(step.anything)", nil, nil, true},
		{"output is bound and empty", "!has(output.anything)", nil, nil, true},
		{"drop is bound with its empty shape", "drop.reason == ''", nil, nil, true},

		// A condition that cannot evaluate must not run the aspect.
		{"a broken condition does not fire the aspect", "input.nope.deeper == 1", nil, nil, false},
		{"a non-boolean condition does not fire the aspect", "input.amount", nil, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.evaluateCondition(ctx, &Config{Name: "test", If: tc.cond}, input, tc.result, tc.flowErr, nil)
			if got != tc.want {
				t.Errorf("condition %q = %v, want %v", tc.cond, got, tc.want)
			}
		})
	}
}

func TestBuildDropInfo(t *testing.T) {
	// on_drop aspects read drop.reason to tell a filtered message from one the
	// accept gate rejected. A nil result must still produce the three keys, or
	// every on_drop condition errors on a missing field.
	info := buildDropInfo(nil)
	for _, k := range []string{"reason", "policy", "message_id"} {
		v, ok := info[k]
		if !ok {
			t.Errorf("a nil result produced no %q key", k)
		}
		if v != "" {
			t.Errorf("%s = %#v, want the empty string", k, v)
		}
	}

	full := buildDropInfo(&flow.FilteredResultWithPolicy{
		Reason:    "filter",
		Policy:    "ack",
		MessageID: "m-1",
	})
	for k, want := range map[string]interface{}{
		"reason": "filter", "policy": "ack", "message_id": "m-1",
	} {
		if full[k] != want {
			t.Errorf("%s = %#v, want %#v", k, full[k], want)
		}
	}
}

func TestGetStringAndIntValue(t *testing.T) {
	// These read flow metadata out of an untyped map. Returning a zero instead
	// of panicking is the point: the metadata is not always present.
	m := map[string]interface{}{
		"s":     "hello",
		"wrong": 42,
		"i64":   int64(7),
		"i":     8,
		"f":     9.9,
		"nan":   "not a number",
	}

	if got := getStringValue(m, "s"); got != "hello" {
		t.Errorf("getStringValue = %q", got)
	}
	if got := getStringValue(m, "missing"); got != "" {
		t.Errorf("getStringValue(missing) = %q, want empty", got)
	}
	if got := getStringValue(m, "wrong"); got != "" {
		t.Errorf("getStringValue on a non-string = %q, want empty", got)
	}

	for key, want := range map[string]int64{"i64": 7, "i": 8, "f": 9} {
		if got := getIntValue(m, key); got != want {
			t.Errorf("getIntValue(%s) = %d, want %d", key, got, want)
		}
	}
	if got := getIntValue(m, "missing"); got != 0 {
		t.Errorf("getIntValue(missing) = %d, want 0", got)
	}
	if got := getIntValue(m, "nan"); got != 0 {
		t.Errorf("getIntValue on a non-number = %d, want 0", got)
	}
}

func TestParseDuration(t *testing.T) {
	// An unparseable duration falls back to five minutes rather than zero.
	// Zero would turn a rate-limit window or a breaker timeout into "no wait
	// at all", which is the opposite of what was configured.
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"", 5 * time.Minute},
		{"nonsense", 5 * time.Minute},
		{"10", 5 * time.Minute}, // no unit is not a duration
	} {
		if got := parseDuration(tc.in); got != tc.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestInterpolateString(t *testing.T) {
	e := newExecutor(t)
	ctx := context.Background()
	input := map[string]interface{}{
		"id":   "order-7",
		"name": "Widget",
		"qty":  3.0,
	}

	for _, tc := range []struct {
		name, template, want string
	}{
		{"no placeholders is passed through", "plain text", "plain text"},
		{"a single placeholder", "Order ${input.id}", "Order order-7"},
		{"several placeholders", "${input.name} x${input.qty}", "Widget x3"},
		{"a placeholder at the start", "${input.id} shipped", "order-7 shipped"},
		{"an expression, not just a lookup", "${input.name + '!'}", "Widget!"},
		{"an empty template", "", ""},
		// An unterminated placeholder must be left alone rather than
		// truncating the message or looping forever.
		{"an unterminated placeholder", "broken ${input.id", "broken ${input.id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.interpolateString(ctx, tc.template, input)
			if err != nil {
				t.Fatalf("interpolateString(%q): %v", tc.template, err)
			}
			if got != tc.want {
				t.Errorf("interpolateString(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestInterpolateStringReportsABadExpression(t *testing.T) {
	// A template referencing something unevaluable must error rather than
	// silently produce a message with a wrong or empty value in it.
	e := newExecutor(t)
	if _, err := e.interpolateString(context.Background(),
		"${input.a.b.c}", map[string]interface{}{}); err == nil {
		t.Error("an unevaluable interpolation was accepted")
	}
}

func TestRegistryRegisterAndAll(t *testing.T) {
	r := NewRegistry()
	if len(r.All()) != 0 {
		t.Errorf("a fresh registry reports %d aspects", len(r.All()))
	}

	if err := r.RegisterAll([]*Config{
		{Name: "audit", When: "after", On: []string{"create_*"},
			Action: &ActionConfig{Connector: "db", Target: "audit_log"}},
		{Name: "notify", When: "on_error", On: []string{"*"},
			Action: &ActionConfig{Connector: "slack"}},
	}); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("registered 2 aspects, registry reports %d", len(all))
	}
	names := map[string]bool{}
	for _, a := range all {
		names[a.Name] = true
	}
	if !names["audit"] || !names["notify"] {
		t.Errorf("registered aspects came back as %#v", names)
	}

	// An invalid aspect must be refused rather than registered half-formed —
	// one with no action would match flows and then do nothing.
	if err := r.RegisterAll([]*Config{{Name: "broken", When: "after", On: []string{"*"}}}); err == nil {
		t.Error("an aspect with no action was accepted")
	}
}

func TestOnDropConditionSeesTheRealDropInfo(t *testing.T) {
	// The whole point of an on_drop aspect is telling one disposition from
	// another. The drop information was built but never put in the activation,
	// so `drop.reason` compared against the empty default and every such
	// condition silently evaluated false.
	e := newExecutor(t)
	ctx := context.Background()
	input := map[string]interface{}{"_flow": "consume"}
	dropInfo := buildDropInfo(&flow.FilteredResultWithPolicy{
		Reason: "sequence_older", Policy: "ack", MessageID: "m-9",
	})

	for _, tc := range []struct {
		cond string
		want bool
	}{
		{"drop.reason == 'sequence_older'", true},
		{"drop.reason == 'filter'", false},
		{"drop.policy == 'ack'", true},
		{"drop.message_id == 'm-9'", true},
	} {
		got := e.evaluateCondition(ctx, &Config{Name: "d", If: tc.cond}, input, nil, nil, dropInfo)
		if got != tc.want {
			t.Errorf("%s = %v, want %v", tc.cond, got, tc.want)
		}
	}
}

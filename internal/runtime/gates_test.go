package runtime

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// The two gates in front of a flow.
//
// A filter decides whether a message is this flow's business at all; an accept
// gate decides whether the business rule holds. Both answer with a boolean and
// both are written as CEL, and getting either wrong is quiet: a message that
// should have been processed is dropped, or one that should have been dropped
// is processed. Neither had a test of what it does with the message itself.

func handlerWithFilter(condition string) *FlowHandler {
	return &FlowHandler{Config: &flow.Config{
		Name: "process_order",
		From: &flow.FromConfig{Connector: "rabbit", Filter: condition},
	}}
}

func TestAFilterDecidesWhetherTheMessageIsOurs(t *testing.T) {
	ctx := context.Background()

	for name, tc := range map[string]struct {
		condition string
		input     map[string]interface{}
		want      bool
	}{
		"a message this flow wants": {
			`input.origin != "internal"`,
			map[string]interface{}{"origin": "shop"},
			true,
		},
		"one it does not": {
			`input.origin != "internal"`,
			map[string]interface{}{"origin": "internal"},
			false,
		},
		"a comparison on a number": {
			`input.total > 1000`,
			map[string]interface{}{"total": 5000},
			true,
		},
		"one that does not hold": {
			`input.total > 1000`,
			map[string]interface{}{"total": 10},
			false,
		},
		"something written about nested data": {
			`input.customer.country == "NZ"`,
			map[string]interface{}{"customer": map[string]interface{}{"country": "NZ"}},
			true,
		},
		"two conditions together": {
			`input.total > 100 && input.status == "paid"`,
			map[string]interface{}{"total": 500, "status": "paid"},
			true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := handlerWithFilter(tc.condition).evaluateFilter(ctx, tc.input)
			if err != nil {
				t.Fatalf("evaluateFilter: %v", err)
			}
			if got != tc.want {
				t.Errorf("%s against %v = %v, want %v", tc.condition, tc.input, got, tc.want)
			}
		})
	}
}

func TestAFlowWithNoFilterTakesEverything(t *testing.T) {
	// The common case, and it must not cost a CEL evaluation per message.
	got, err := handlerWithFilter("").evaluateFilter(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("evaluateFilter: %v", err)
	}
	if !got {
		t.Error("a flow with no filter refused a message")
	}
}

func TestAFilterWrittenAsABlockIsTheOneUsed(t *testing.T) {
	// Both forms exist. A flow carrying both and honouring the older attribute
	// would apply a condition its author replaced.
	handler := &FlowHandler{Config: &flow.Config{
		Name: "process_order",
		From: &flow.FromConfig{
			Connector:    "rabbit",
			Filter:       `input.origin != "internal"`,
			FilterConfig: &flow.FilterConfig{Condition: `input.total > 1000`},
		},
	}}

	// The block's condition holds, the attribute's does not: the message is
	// internal and large.
	got, err := handler.evaluateFilter(context.Background(), map[string]interface{}{
		"origin": "internal", "total": 5000,
	})
	if err != nil {
		t.Fatalf("evaluateFilter: %v", err)
	}
	if !got {
		t.Error("the filter block was written and the older attribute was applied instead")
	}
}

func TestAFilterThatCannotBeEvaluated(t *testing.T) {
	// A field the message does not carry, or an expression that does not
	// compile. Answering "true" would process a message nobody meant to
	// process, and answering "false" silently would drop it — so it is an
	// error, and the flow's error handling decides.
	for name, tc := range map[string]struct {
		condition string
		input     map[string]interface{}
	}{
		"a field that is not there": {`input.nothing.at.all == "x"`, map[string]interface{}{}},
		"not an expression at all":  {`this is not CEL`, map[string]interface{}{}},
	} {
		t.Run(name, func(t *testing.T) {
			accepted, err := handlerWithFilter(tc.condition).evaluateFilter(context.Background(), tc.input)
			if err == nil {
				t.Fatalf("a filter that could not be evaluated answered %v", accepted)
			}
		})
	}
}

func TestAnAcceptGateIsTheBusinessRule(t *testing.T) {
	// Where a filter says "not my message", accept says "my message, and the
	// answer is no" — a consumer that sees the order but declines it, which
	// is what lets several services share one queue.
	handler := func(when string) *FlowHandler {
		return &FlowHandler{Config: &flow.Config{
			Name:   "ship_order",
			From:   &flow.FromConfig{Connector: "rabbit"},
			Accept: &flow.AcceptConfig{When: when},
		}}
	}
	ctx := context.Background()

	yes, err := handler(`input.warehouse == "auckland"`).evaluateAccept(ctx,
		map[string]interface{}{"warehouse": "auckland"})
	if err != nil {
		t.Fatalf("evaluateAccept: %v", err)
	}
	if !yes {
		t.Error("an order this service handles was declined")
	}

	no, err := handler(`input.warehouse == "auckland"`).evaluateAccept(ctx,
		map[string]interface{}{"warehouse": "wellington"})
	if err != nil {
		t.Fatalf("evaluateAccept: %v", err)
	}
	if no {
		t.Error("an order for another warehouse was accepted")
	}

	// No gate at all accepts everything.
	all, err := (&FlowHandler{Config: &flow.Config{Name: "ship_order"}}).evaluateAccept(ctx, nil)
	if err != nil || !all {
		t.Errorf("a flow with no accept gate answered %v, %v", all, err)
	}
	empty, err := handler("").evaluateAccept(ctx, nil)
	if err != nil || !empty {
		t.Errorf("an empty accept gate answered %v, %v", empty, err)
	}

	// And one that cannot be evaluated is an error rather than a silent yes.
	if _, err := handler(`input.nothing.at.all`).evaluateAccept(ctx, map[string]interface{}{}); err == nil {
		t.Error("an accept gate that could not be evaluated let the message through")
	}
}

func TestOnlyTheStepsTheAnswerNeeds(t *testing.T) {
	// A GraphQL query asking for two fields should not run the steps that
	// fetch the other six. Getting it wrong in one direction wastes calls; in
	// the other it returns null for a field somebody asked for.
	handler := &FlowHandler{Config: &flow.Config{
		Name: "user",
		Steps: []*flow.StepConfig{
			{Name: "profile", Connector: "db"},
			{Name: "orders", Connector: "db"},
			{Name: "invoices", Connector: "db"},
		},
		Transform: &flow.TransformConfig{Mappings: map[string]string{
			"name":       "step.profile.name",
			"orders":     "step.orders",
			"invoice_id": "step.invoices.id",
		}},
	}}

	needed := handler.analyzeNeededSteps(map[string]interface{}{
		"__requested_top_fields": []string{"name", "orders"},
	})
	if needed == nil {
		t.Fatal("nothing was worked out, so every step runs")
	}
	if !needed["profile"] || !needed["orders"] {
		t.Errorf("a step the answer needs was skipped: %v", needed)
	}
	if needed["invoices"] {
		t.Errorf("a step nothing asked for was run: %v", needed)
	}
}

func TestEveryStepRunsWhenNobodySaidWhatWasAsked(t *testing.T) {
	// Anything that is not a GraphQL query — a queue message, a REST call —
	// carries no field list, and the optimisation must then not apply.
	// Guessing here would drop a step whose write is the whole point.
	handler := &FlowHandler{Config: &flow.Config{
		Name:  "user",
		Steps: []*flow.StepConfig{{Name: "profile", Connector: "db"}},
		Transform: &flow.TransformConfig{Mappings: map[string]string{
			"name": "step.profile.name",
		}},
	}}

	for name, input := range map[string]map[string]interface{}{
		"no field list":       {},
		"an empty field list": {"__requested_top_fields": []string{}},
		"a field list of the wrong kind": {
			"__requested_top_fields": "name",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if needed := handler.analyzeNeededSteps(input); needed != nil {
				t.Errorf("steps were skipped with nothing to go on: %v", needed)
			}
		})
	}

	// And a flow with no transform: there is nothing to trace a field back
	// through, so every step runs.
	plain := &FlowHandler{Config: &flow.Config{
		Name:  "user",
		Steps: []*flow.StepConfig{{Name: "profile", Connector: "db"}},
	}}
	if needed := plain.analyzeNeededSteps(map[string]interface{}{
		"__requested_top_fields": []string{"name"},
	}); needed != nil {
		t.Errorf("steps were skipped in a flow with no transform: %v", needed)
	}
}

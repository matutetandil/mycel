package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A flow made of steps gathers from several places and then says what to send
// back. Gathering is covered elsewhere; this is about the answer — what a
// caller actually receives, which is the part they see.

func stepsFlow(t *testing.T, mappings map[string]string, order []string) *FlowHandler {
	t.Helper()
	customers := &stepConnector{name: "customers", rows: []map[string]interface{}{
		{"id": "c-1", "name": "Ada", "tier": "gold"},
	}}
	orders := &stepConnector{name: "orders", rows: []map[string]interface{}{
		{"id": "o-9", "total": 42},
	}}

	handler := stepHandler(t, []*flow.StepConfig{
		{
			Name: "customer", Connector: "customers",
			ConnectorParams: map[string]interface{}{"query": "SELECT * FROM customers"},
		},
		{
			Name: "latest_order", Connector: "orders",
			ConnectorParams: map[string]interface{}{"query": "SELECT * FROM orders"},
		},
	}, map[string]connector.Connector{"customers": customers, "orders": orders})

	if mappings != nil {
		handler.Config.Transform = &flow.TransformConfig{Mappings: mappings, Order: order}
	}
	return handler
}

func TestTheAnswerIsShapedByTheTransform(t *testing.T) {
	handler := stepsFlow(t, map[string]string{
		"customer_name": "step.customer.name",
		"order_total":   "step.latest_order.total",
	}, []string{"customer_name", "order_total"})

	answer, _, err := handler.handleStepsFlow(context.Background(), map[string]interface{}{"id": "c-1"})
	if err != nil {
		t.Fatalf("handleStepsFlow: %v", err)
	}

	fields, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("the answer came back as %T", answer)
	}
	if fields["customer_name"] != "Ada" {
		t.Errorf("answer = %v, want the field the transform names", fields)
	}
	if fields["order_total"] != int64(42) && fields["order_total"] != 42 {
		t.Errorf("order_total = %#v", fields["order_total"])
	}
}

func TestTheAnswerIsTheSameOnEveryRequest(t *testing.T) {
	// With no transform the flow hands back a step's result, and it used to
	// pick one by walking a map: three steps meant three possible answers, a
	// different one per request, with nothing in the configuration to explain
	// why the same call returned a customer once and an order the next time.
	first, _, err := stepsFlow(t, nil, nil).handleStepsFlow(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleStepsFlow: %v", err)
	}

	for i := 0; i < 25; i++ {
		again, _, err := stepsFlow(t, nil, nil).handleStepsFlow(context.Background(), map[string]interface{}{})
		if err != nil {
			t.Fatalf("handleStepsFlow: %v", err)
		}
		if !sameAnswer(first, again) {
			t.Fatalf("the same request answered %v and then %v", first, again)
		}
	}
}

func TestWithNoTransformTheAnswerIsTheFirstStep(t *testing.T) {
	// First as written, which is the only order a reader can predict.
	answer, _, err := stepsFlow(t, nil, nil).handleStepsFlow(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleStepsFlow: %v", err)
	}

	fields, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("the answer came back as %T", answer)
	}
	if fields["name"] != "Ada" {
		t.Errorf("answer = %v, want the result of the step written first", fields)
	}
}

func TestAStepThatFailsIsReportedRatherThanAnswered(t *testing.T) {
	broken := &stepConnector{name: "customers", err: errStepFailed}
	handler := stepHandler(t, []*flow.StepConfig{{
		Name: "customer", Connector: "customers",
		ConnectorParams: map[string]interface{}{"query": "SELECT * FROM customers"},
	}}, map[string]connector.Connector{"customers": broken})

	if _, _, err := handler.handleStepsFlow(context.Background(), map[string]interface{}{}); err == nil {
		t.Error("a flow whose step failed answered as though it had not")
	}
}

func sameAnswer(a, b interface{}) bool {
	first, aok := a.(map[string]interface{})
	second, bok := b.(map[string]interface{})
	if !aok || !bok {
		return a == b
	}
	if len(first) != len(second) {
		return false
	}
	for k, v := range first {
		if second[k] != v {
			return false
		}
	}
	return true
}

var errStepFailed = &stepFailure{}

type stepFailure struct{}

func (*stepFailure) Error() string { return "the customer service is down" }

// A transform can be nothing but a reference to a named one. That is the case
// steps make most useful — several flows reading different rows into one shape
// declared once — and it was the case that did nothing: the steps path decided
// whether a transform existed by counting inline mappings, found none, and
// answered the first step's raw rows. No error, nothing in the log, and the
// same block worked in a flow without steps.

func stepsFlowUsing(t *testing.T, use string, inline map[string]string) *FlowHandler {
	t.Helper()
	handler := stepsFlow(t, nil, nil)
	handler.Config.Transform = &flow.TransformConfig{Use: use, Mappings: inline}
	handler.NamedTransforms = map[string]*transform.Config{
		"shape": {
			Name: "shape",
			Mappings: map[string]string{
				"customer_name": "step.customer.name",
				"order_total":   "step.latest_order.total",
			},
			Order: []string{"customer_name", "order_total"},
		},
	}
	return handler
}

func TestAUseOnlyTransformShapesTheAnswerOfAStepsFlow(t *testing.T) {
	answer, _, err := stepsFlowUsing(t, "shape", nil).handleStepsFlow(context.Background(), map[string]interface{}{"id": "c-1"})
	if err != nil {
		t.Fatalf("handleStepsFlow: %v", err)
	}

	fields, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("the answer came back as %T", answer)
	}
	if fields["customer_name"] != "Ada" {
		t.Errorf("answer = %v, want the shape the named transform declares, not the first step's rows", fields)
	}
	if _, raw := fields["tier"]; raw {
		t.Errorf("answer = %v carries the step's own columns: the named transform was ignored", fields)
	}
}

func TestInlineMappingsLayerOverTheNamedTransformInAStepsFlow(t *testing.T) {
	// `use` plus an inline field used to fail at request time with "no such
	// key": the inline half was evaluated without the named half's fields.
	answer, _, err := stepsFlowUsing(t, "shape", map[string]string{
		"summary": "output.customer_name + ' owes ' + string(output.order_total)",
	}).handleStepsFlow(context.Background(), map[string]interface{}{"id": "c-1"})
	if err != nil {
		t.Fatalf("handleStepsFlow: %v", err)
	}

	fields, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("the answer came back as %T", answer)
	}
	if fields["summary"] != "Ada owes 42" {
		t.Errorf("summary = %#v, want the inline field to see the named transform's output", fields["summary"])
	}
	if fields["customer_name"] != "Ada" {
		t.Errorf("answer = %v lost the named transform's own fields", fields)
	}
}

func TestATransformNamingNothingIsAnErrorNotTheRawStep(t *testing.T) {
	_, _, err := stepsFlowUsing(t, "no_such_shape", nil).handleStepsFlow(context.Background(), map[string]interface{}{"id": "c-1"})
	if err == nil {
		t.Fatal("a transform using a name nobody declared answered as though it had not")
	}
	if !strings.Contains(err.Error(), "no_such_shape") {
		t.Errorf("error = %q, want it to name the missing transform", err)
	}
}

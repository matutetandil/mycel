package runtime

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
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

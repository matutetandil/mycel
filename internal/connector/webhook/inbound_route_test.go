package webhook

import (
	"context"
	"testing"
)

// The inbound connector verifies signatures, checks the allowed addresses and
// builds an event — and nothing served it or read from it. Its handler was set
// by nobody, and the server that mounts its path was never constructed, so a
// webhook connector could be configured, could start, and could not receive.

func TestRegisterRouteDeliversTheEventToTheFlow(t *testing.T) {
	c := NewInboundConnector("hooks", DefaultInboundConfig())

	var got map[string]interface{}
	c.RegisterRoute("", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		got = input
		return nil, nil
	})

	event := &WebhookEvent{
		ID:      "evt-1",
		Type:    "payment.succeeded",
		Source:  "203.0.113.7",
		Headers: map[string]string{"X-Signature": "abc"},
		Payload: map[string]interface{}{"amount": 100},
	}
	if err := c.handler(context.Background(), event); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if got == nil {
		t.Fatal("the flow was never called")
	}
	if got["id"] != "evt-1" || got["type"] != "payment.succeeded" {
		t.Errorf("input = %#v", got)
	}
	// The payload arrives under body, the shape every other source uses.
	body, ok := got["body"].(map[string]interface{})
	if !ok || body["amount"] != 100 {
		t.Errorf("body = %#v", got["body"])
	}
	if headers, ok := got["headers"].(map[string]string); !ok || headers["X-Signature"] != "abc" {
		t.Errorf("headers = %#v", got["headers"])
	}
}

func TestAFlowErrorIsReportedToTheSender(t *testing.T) {
	// A webhook sender retries on a failure status, so a flow that could not
	// process the event must not be answered with a 200.
	c := NewInboundConnector("hooks", DefaultInboundConfig())
	c.RegisterRoute("", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, context.DeadlineExceeded
	})

	err := c.handler(context.Background(), &WebhookEvent{ID: "evt-2"})
	if err == nil {
		t.Error("a failing flow reported success to the sender")
	}
}

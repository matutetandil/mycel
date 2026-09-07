package saga

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A saga action makes the same call a step does, so it declares request
// headers the same way — and until it did, `headers` written on an action
// was refused by the parser as an unknown attribute, while the same block on
// a step worked.
type headerSeeingCaller struct {
	mockConnector
	seen []map[string]string
}

func (c *headerSeeingCaller) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	c.seen = append(c.seen, connector.RequestHeaders(ctx))
	return c.mockConnector.Call(ctx, operation, params)
}

func TestASagaActionSendsTheHeadersItDeclares(t *testing.T) {
	api := &headerSeeingCaller{mockConnector: mockConnector{name: "stripe", failAt: -1}}
	exec := NewExecutor(&mockRegistry{connectors: map[string]connector.Connector{"stripe": api}})

	_, err := exec.ExecuteAction(context.Background(), &ActionConfig{
		Connector: "stripe",
		Operation: "POST /charges",
		Body:      map[string]interface{}{"amount": 100},
		Headers: map[string]interface{}{
			"Store":           "input.store",
			"Idempotency-Key": "step.order.id",
			"X-Fixed":         "yes",
			"X-Absent":        "input.nope",
		},
	}, map[string]interface{}{"store": "au", "nope": nil}, map[string]interface{}{"order": map[string]interface{}{"id": "o-1"}})
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}
	if len(api.seen) != 1 {
		t.Fatalf("the connector was called %d times", len(api.seen))
	}
	got := api.seen[0]
	if got["Store"] != "au" || got["Idempotency-Key"] != "o-1" || got["X-Fixed"] != "yes" {
		t.Errorf("headers = %v", got)
	}
	if _, sent := got["X-Absent"]; sent {
		t.Errorf("a null header was sent as %q", got["X-Absent"])
	}
}

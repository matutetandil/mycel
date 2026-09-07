package statemachine

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A transition's action makes the same call a step does, so it declares
// request headers the same way.
type headerSeeingCaller struct {
	mockConnector
	seen []map[string]string
}

func (c *headerSeeingCaller) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	c.seen = append(c.seen, connector.RequestHeaders(ctx))
	return c.mockConnector.Call(ctx, operation, params)
}

func TestATransitionActionSendsTheHeadersItDeclares(t *testing.T) {
	api := &headerSeeingCaller{mockConnector: mockConnector{name: "erp"}}
	engine := NewEngine(&mockRegistry{connectors: map[string]connector.Connector{"erp": api}})

	_, err := engine.executeAction(context.Background(), &ActionConfig{
		Connector: "erp",
		Operation: "POST /ship",
		Body:      map[string]interface{}{"id": "input.id"},
		Headers:   map[string]interface{}{"X-Tenant": "input.tenant", "X-Fixed": "yes"},
	}, map[string]interface{}{"id": "o-1", "tenant": "acme"})
	if err != nil {
		t.Fatalf("executeAction: %v", err)
	}
	if len(api.seen) != 1 || api.seen[0]["X-Tenant"] != "acme" || api.seen[0]["X-Fixed"] != "yes" {
		t.Errorf("headers = %v", api.seen)
	}
}

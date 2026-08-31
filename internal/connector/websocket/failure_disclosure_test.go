package websocket

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWhatAProductionSocketIsToldAboutAFailure(t *testing.T) {
	// The REST connector was the only server that asked what environment it
	// was running in. This one sent the raw error text down the socket
	// wherever it ran, and a database error is exactly the kind that carries
	// the name of a table.
	c, address := serverOn(t)
	c.environment = "production"

	c.RegisterRoute("message", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("query failed: no such table: internal_billing_table")
	})

	client := dial(t, address, "")
	waitForClients(t, c, 1)
	send(t, client, Message{Type: "message", Data: map[string]interface{}{"order_id": "order-1"}})

	message, ok := nextMessage(t, client)
	if !ok {
		t.Fatal("the client was told nothing at all")
	}
	if message["type"] != "error" {
		t.Errorf("a failure came back as %v", message["type"])
	}
	if text, _ := message["message"].(string); strings.Contains(text, "internal_billing_table") {
		t.Errorf("the name of an internal table was sent to the caller: %q", text)
	}
}

func TestWhatADevelopmentSocketIsToldAboutAFailure(t *testing.T) {
	c, address := serverOn(t)

	c.RegisterRoute("message", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("query failed: no such table: internal_billing_table")
	})

	client := dial(t, address, "")
	waitForClients(t, c, 1)
	send(t, client, Message{Type: "message", Data: map[string]interface{}{"order_id": "order-1"}})

	message, ok := nextMessage(t, client)
	if !ok {
		t.Fatal("the client was told nothing at all")
	}
	if text, _ := message["message"].(string); !strings.Contains(text, "internal_billing_table") {
		t.Errorf("a developer was not told what actually failed: %q", text)
	}
}

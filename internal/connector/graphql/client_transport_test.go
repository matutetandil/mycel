package graphql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
)

// The subscription client's transport, checked against a gorilla server so the
// peer is a different implementation than the client's own library.
//
// Three things the move from gorilla to coder/websocket could have changed
// without any existing test noticing: an event larger than the new library's
// 32KB default read limit, the headers configured on the connector reaching
// the handshake, and the client unsubscribing when it shuts down. And the
// selection a flow declares reaching the subscribe message.
func TestSubscriptionClientTransport(t *testing.T) {
	upgrader := gorilla.Upgrader{
		CheckOrigin:  func(*http.Request) bool { return true },
		Subprotocols: []string{"graphql-transport-ws"},
	}

	tenant := make(chan string, 1)
	sentQuery := make(chan string, 1)
	sawComplete := make(chan string, 1)
	blob := strings.Repeat("x", 100*1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant <- r.Header.Get("X-Tenant")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var msg wsMessage
		if conn.ReadJSON(&msg) != nil || msg.Type != msgConnectionInit {
			return
		}
		_ = conn.WriteJSON(wsMessage{Type: msgConnectionAck})
		if conn.ReadJSON(&msg) != nil || msg.Type != msgSubscribe {
			return
		}
		subID := msg.ID
		var sub subscribePayload
		_ = json.Unmarshal(msg.Payload, &sub)
		sentQuery <- sub.Query

		payload, _ := json.Marshal(map[string]interface{}{
			"data": map[string]interface{}{"orderUpdated": map[string]interface{}{"note": blob}},
		})
		_ = conn.WriteJSON(wsMessage{ID: subID, Type: msgNext, Payload: payload})

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == msgComplete {
				sawComplete <- msg.ID
				return
			}
		}
	}))
	defer server.Close()

	client := NewClient("sub-transport", &ClientConfig{
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
		Headers:  map[string]string{"X-Tenant": "acme"},
		Subscriptions: &SubscriptionsConfig{
			Enabled:           true,
			ConnectionTimeout: 5 * time.Second,
		},
	}, nil)

	got := make(chan int, 1)
	client.RegisterRoute("Subscription.orderUpdated", func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		note, _ := input["note"].(string)
		got <- len(note)
		return nil, nil
	})

	// What a flow's `selection` becomes: a field typed as an object is only
	// answered when the subscription names its fields.
	client.SetOperationSelection("Subscription.orderUpdated", "note")

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case h := <-tenant:
		if h != "acme" {
			t.Errorf("handshake carried X-Tenant %q, want acme", h)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the client never connected")
	}

	select {
	case q := <-sentQuery:
		if q != "subscription { orderUpdated { note } }" {
			t.Errorf("subscribed with %q, want the flow's selection in it", q)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the client never subscribed")
	}

	select {
	case n := <-got:
		if n != len(blob) {
			t.Fatalf("handler got %d bytes, want %d", n, len(blob))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a 100KB event never reached the handler")
	}

	if err := client.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-sawComplete:
		if id != "sub-orderUpdated" {
			t.Errorf("complete for %q, want sub-orderUpdated", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutting down did not unsubscribe with complete")
	}
}

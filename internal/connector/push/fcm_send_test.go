package push

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"context"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Sending a notification to a phone.
//
// What a device receives is decided here, and none of it was exercised: which
// devices a message is addressed to, whether it arrives as a notification the
// system shows or as data the app handles, and — the one that matters over
// time — which tokens the service told us are dead. A token nobody removes is
// a notification sent for ever to an app that was uninstalled.

type fcmAPI struct {
	status int
	answer string
	calls  int32

	lastBody map[string]interface{}
	lastAuth string
}

func (f *fcmAPI) serve(t *testing.T) *FCMConnector {
	t.Helper()
	if f.status == 0 {
		f.status = http.StatusOK
	}
	if f.answer == "" {
		f.answer = `{"message_id":123,"success":1,"failure":0}`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.calls, 1)
		f.lastAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&f.lastBody)
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.answer))
	}))
	t.Cleanup(server.Close)

	return NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM:    &FCMConfig{ServerKey: "the-server-key", APIURL: server.URL},
	})
}

func TestWhoANotificationIsAddressedTo(t *testing.T) {
	// Four ways, and sending to the wrong one is either nobody or everybody.
	for name, tc := range map[string]struct {
		message *Message
		check   func(t *testing.T, sent map[string]interface{})
	}{
		"one device": {
			&Message{Token: "device-1", Title: "Your order", Body: "On its way"},
			func(t *testing.T, sent map[string]interface{}) {
				if sent["to"] != "device-1" {
					t.Errorf("to = %v", sent["to"])
				}
			},
		},
		"several devices": {
			&Message{Tokens: []string{"device-1", "device-2"}, Title: "Your order"},
			func(t *testing.T, sent map[string]interface{}) {
				ids, ok := sent["registration_ids"].([]interface{})
				if !ok || len(ids) != 2 {
					t.Errorf("registration ids = %v, want both", sent["registration_ids"])
				}
			},
		},
		"everybody subscribed to a topic": {
			&Message{Topic: "orders", Title: "Your order"},
			func(t *testing.T, sent map[string]interface{}) {
				// The prefix is what tells FCM this is a topic and not a
				// device token, and without it the message goes nowhere.
				if sent["to"] != "/topics/orders" {
					t.Errorf("to = %v, want the topic form", sent["to"])
				}
			},
		},
		"whoever matches a condition": {
			&Message{Condition: "'orders' in topics", Title: "Your order"},
			func(t *testing.T, sent map[string]interface{}) {
				if sent["condition"] == nil {
					t.Errorf("sent = %v, want the condition", sent)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			api := &fcmAPI{}
			c := api.serve(t)

			if _, err := c.Send(context.Background(), tc.message); err != nil {
				t.Fatalf("Send: %v", err)
			}
			tc.check(t, api.lastBody)
		})
	}
}

func TestWhatTheDeviceIsSent(t *testing.T) {
	api := &fcmAPI{}
	c := api.serve(t)

	if _, err := c.Send(context.Background(), &Message{
		Token:       "device-1",
		Title:       "Your order",
		Body:        "On its way",
		Data:        map[string]string{"order_id": "order-1"},
		Priority:    "high",
		TTL:         3600,
		CollapseKey: "orders",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The key travels as the credential; without it every send is refused
	// with something that names neither the service nor the message.
	if api.lastAuth != "key=the-server-key" {
		t.Errorf("authorization = %q", api.lastAuth)
	}

	notification, ok := api.lastBody["notification"].(map[string]interface{})
	if !ok || notification["title"] != "Your order" {
		t.Errorf("notification = %v", api.lastBody["notification"])
	}
	// Data is what the app reads when it handles the message itself.
	data, ok := api.lastBody["data"].(map[string]interface{})
	if !ok || data["order_id"] != "order-1" {
		t.Errorf("data = %v", api.lastBody["data"])
	}
	// High priority is what wakes a sleeping phone, and a message that needed
	// it and did not get it arrives whenever.
	if api.lastBody["priority"] != "high" {
		t.Errorf("priority = %v", api.lastBody["priority"])
	}
	if api.lastBody["time_to_live"] == nil {
		t.Error("no time to live, so a message outlives what it is about")
	}
	// The collapse key is what stops five notifications about the same order.
	if api.lastBody["collapse_key"] != "orders" {
		t.Errorf("collapse key = %v", api.lastBody["collapse_key"])
	}
}

func TestTheDevicesThatCouldNotBeReachedAreNamed(t *testing.T) {
	// A token dies when an app is uninstalled, and a service that never learns
	// which ones sends to them for ever.
	api := &fcmAPI{
		answer: `{"message_id":123,"success":1,"failure":1,"results":[
			{"message_id":"m-1"},
			{"error":"NotRegistered"}
		]}`,
	}
	c := api.serve(t)

	result, err := c.Send(context.Background(), &Message{
		Tokens: []string{"device-1", "device-2"},
		Title:  "Your order",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(result.FailedTokens) != 1 || result.FailedTokens[0] != "device-2" {
		t.Errorf("failed tokens = %v, want the one the service refused", result.FailedTokens)
	}
	// The send is still a success: one device got it.
	if !result.Success {
		t.Error("a send that reached one of two devices was reported as a failure")
	}
}

func TestASendTheServiceRefusedIsAFailure(t *testing.T) {
	// A key that was rotated or a quota: a flow reading these as sent will
	// never send the notification again.
	api := &fcmAPI{status: http.StatusUnauthorized, answer: `{"error":"InvalidServerKey"}`}
	c := api.serve(t)

	result, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"})
	if err == nil {
		t.Fatal("a send the service refused was reported as delivered")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v", result)
	}
}

func TestAWriteBecomesTheNotification(t *testing.T) {
	// The path a flow takes: a `to` block with a push connector in it.
	api := &fcmAPI{}
	c := api.serve(t)

	result, err := c.Write(context.Background(), &connector.Data{
		Target: "device-1",
		Payload: map[string]interface{}{
			"title": "Your order",
			"body":  "On its way",
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result == nil || len(result.Rows) == 0 {
		t.Errorf("result = %+v", result)
	}
	if api.lastBody["to"] != "device-1" {
		t.Errorf("to = %v, want the flow's target", api.lastBody["to"])
	}
}

func TestAWriteThatCouldNotBeSentFailsTheFlow(t *testing.T) {
	api := &fcmAPI{status: http.StatusInternalServerError, answer: `{"error":"Unavailable"}`}
	c := api.serve(t)

	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "device-1",
		Payload: map[string]interface{}{"title": "Your order"},
	}); err == nil {
		t.Error("a notification nobody received was reported as sent")
	}
}

func TestAServiceThatCannotBeReachedIsReported(t *testing.T) {
	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM:    &FCMConfig{ServerKey: "the-server-key", APIURL: "http://127.0.0.1:1"},
	})

	if _, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"}); err == nil {
		t.Error("a notification was sent to a service nobody is running")
	}
}

func TestTheAddressIsTidiedUp(t *testing.T) {
	// A trailing slash in the configuration would produce //fcm/send, which
	// some servers answer and others do not.
	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM:    &FCMConfig{ServerKey: "k", APIURL: "https://fcm.example.com/"},
	})
	if c.config.FCM.APIURL != "https://fcm.example.com" {
		t.Errorf("api url = %q", c.config.FCM.APIURL)
	}

	// And nothing said is the real service.
	plain := NewFCMConnector("notifications", &Config{Driver: "fcm", FCM: &FCMConfig{ServerKey: "k"}})
	if plain.config.FCM.APIURL == "" {
		t.Error("no address at all, so every send goes nowhere")
	}
	if plain.config.FCM.Timeout == 0 {
		t.Error("no timeout, so a service that never answers holds the flow for ever")
	}
}

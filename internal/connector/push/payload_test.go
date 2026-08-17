package push

import (
	"testing"
)

// A flow hands a push connector a map, because that is what a transform
// produces and what JSON decodes into. Everything a notification carries has to
// survive that hand-off: the title and body a person reads, and the data an app
// reads to know what to open — an order id, a deep link, a conversation.
//
// The data payload is the half nobody sees go missing. The notification still
// arrives and still says the right thing; tapping it just lands on the wrong
// screen, or nowhere.

func TestEverythingAFlowSendsArrivesInTheNotification(t *testing.T) {
	msg, err := pushFromData("", map[string]interface{}{
		"token": "device-1",
		"title": "Your order shipped",
		"body":  "Arriving Thursday",
		"data": map[string]interface{}{
			"order_id": "A-1234",
			"screen":   "order_detail",
		},
	})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}

	if msg.Token != "device-1" {
		t.Errorf("token = %q", msg.Token)
	}
	if msg.Title != "Your order shipped" || msg.Body != "Arriving Thursday" {
		t.Errorf("message = %+v", msg)
	}

	// The part that was being dropped.
	if len(msg.Data) == 0 {
		t.Fatal("the data payload was dropped, so the app has nothing to act on")
	}
	if msg.Data["order_id"] != "A-1234" || msg.Data["screen"] != "order_detail" {
		t.Errorf("data = %v", msg.Data)
	}
}

func TestDataValuesThatAreNotTextAreCarriedAsText(t *testing.T) {
	// A transform produces numbers and booleans, and a push payload is text
	// either way — the platform only carries strings. Dropping them would lose
	// exactly the identifiers an app needs.
	msg, err := pushFromData("device-1", map[string]interface{}{
		"body": "Your order shipped",
		"data": map[string]interface{}{
			"order_id": 1234,
			"urgent":   true,
			"total":    10.5,
		},
	})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}

	for field, want := range map[string]string{
		"order_id": "1234",
		"urgent":   "true",
		"total":    "10.5",
	} {
		if got := msg.Data[field]; got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}

func TestAlreadyTypedDataIsTakenAsWell(t *testing.T) {
	// A caller inside the process may hand over the map already typed.
	msg, err := pushFromData("device-1", map[string]interface{}{
		"data": map[string]string{"order_id": "A-1234"},
	})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}
	if msg.Data["order_id"] != "A-1234" {
		t.Errorf("data = %v", msg.Data)
	}
}

func TestSendingToSeveralDevicesAtOnce(t *testing.T) {
	// One person with a phone and a tablet, or a broadcast to a small list.
	// Without this a flow can only ever reach one device.
	msg, err := pushFromData("", map[string]interface{}{
		"tokens": []interface{}{"device-1", "device-2"},
		"body":   "Your order shipped",
	})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}
	if len(msg.Tokens) != 2 || msg.Tokens[0] != "device-1" {
		t.Errorf("tokens = %v", msg.Tokens)
	}
}

func TestTheDeliveryOptionsAFlowCanSet(t *testing.T) {
	msg, err := pushFromData("", map[string]interface{}{
		"token":        "device-1",
		"body":         "Your order shipped",
		"priority":     "high",
		"ttl":          3600,
		"collapse_key": "order-A-1234",
		"condition":    "'orders' in topics",
	})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}
	if msg.Priority != "high" {
		t.Errorf("priority = %q, want the urgency the flow asked for", msg.Priority)
	}
	if msg.TTL != 3600 {
		t.Errorf("ttl = %d, want how long it is worth delivering", msg.TTL)
	}
	if msg.CollapseKey != "order-A-1234" {
		t.Errorf("collapse key = %q, want the one that replaces an earlier notice", msg.CollapseKey)
	}
	if msg.Condition != "'orders' in topics" {
		t.Errorf("condition = %q", msg.Condition)
	}
}

func TestTheTargetIsTheDeviceWhenTheBodyDoesNotNameOne(t *testing.T) {
	// `to { connector = "push", target = "<token>" }` is the short form.
	msg, err := pushFromData("device-1", "Your order shipped")
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}
	if msg.Token != "device-1" || msg.Body != "Your order shipped" {
		t.Errorf("message = %+v", msg)
	}

	// And a token in the payload wins over the target, since it is the more
	// specific of the two.
	msg, err = pushFromData("device-1", map[string]interface{}{"token": "device-2", "body": "hi"})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}
	if msg.Token != "device-2" {
		t.Errorf("token = %q, want the one in the payload", msg.Token)
	}
}

func TestATopicIsSentToRatherThanADevice(t *testing.T) {
	msg, err := pushFromData("", map[string]interface{}{
		"topic": "orders",
		"body":  "Your order shipped",
	})
	if err != nil {
		t.Fatalf("pushFromData: %v", err)
	}
	if msg.Topic != "orders" {
		t.Errorf("topic = %q", msg.Topic)
	}
}

func TestSomethingThatIsNotANotificationIsRefused(t *testing.T) {
	if _, err := pushFromData("device-1", 42); err == nil {
		t.Error("a number was accepted as a notification")
	}
}

package connector

import "testing"

// Addressing whoever is connected.
//
// The push connectors take their addressee from the message — a room from the
// path, a user id from the body — and the documentation writes that the way
// everything else in Mycel refers to a field: target = "input.user_id".
// Nothing evaluated it, so the message went to a user of that literal name.
// Verified against a running service: a client that connected as
// "input.user_id" received the notification meant for user 42, and the send was
// reported as delivered.

func TestATargetNamingAFieldIsResolved(t *testing.T) {
	payload := map[string]interface{}{"user_id": "42", "room": "orders"}

	if got := ResolveTarget("input.user_id", payload); got != "42" {
		t.Errorf("input.user_id resolved to %q", got)
	}
	if got := ResolveTarget("input.room", payload); got != "orders" {
		t.Errorf("input.room resolved to %q", got)
	}
}

func TestALiteralTargetIsLeftAlone(t *testing.T) {
	// A room named in the configuration still addresses that room, and a table
	// name is not a path into the payload.
	payload := map[string]interface{}{"orders": "not this"}

	for _, target := range []string{"orders", "users", "notifications.high"} {
		if got := ResolveTarget(target, payload); got != target {
			t.Errorf("literal target %q became %q", target, got)
		}
	}
}

func TestAnIdentifierThatIsANumberBecomesItsDigits(t *testing.T) {
	// JSON makes every number a float, and an id from a database is as likely
	// to be a number as a word — 42 must not address user "42.000000" or "%!d".
	payload := map[string]interface{}{"user_id": float64(42), "other": 7}

	if got := ResolveTarget("input.user_id", payload); got != "42" {
		t.Errorf("a numeric id resolved to %q", got)
	}
	if got := ResolveTarget("input.other", payload); got != "7" {
		t.Errorf("an integer id resolved to %q", got)
	}
}

func TestANestedFieldIsReached(t *testing.T) {
	payload := map[string]interface{}{"body": map[string]interface{}{"user_id": "7"}}

	if got := ResolveTarget("input.body.user_id", payload); got != "7" {
		t.Errorf("a nested field resolved to %q", got)
	}
}

func TestAFieldThatIsNotThereLeavesTheTargetAlone(t *testing.T) {
	// Rather than resolving to nothing and delivering to everyone or to no one
	// without saying so: the connector reports an empty addressee itself.
	if got := ResolveTarget("input.missing", map[string]interface{}{}); got != "input.missing" {
		t.Errorf("an absent field resolved to %q", got)
	}
	if got := ResolveTarget("input.user_id", nil); got != "input.user_id" {
		t.Errorf("with no payload the target became %q", got)
	}
}

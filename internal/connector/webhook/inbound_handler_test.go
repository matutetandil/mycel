package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A webhook endpoint is a door onto the internet that anybody can knock on. The
// signature is the only thing separating the provider from everybody else, so
// every way of getting past it without one has to be closed.

func listening(t *testing.T, config *InboundConfig) *InboundConnector {
	t.Helper()
	c := NewInboundConnector("stripe_webhook", config)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func signed(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func deliver(c *InboundConnector, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "203.0.113.7:44321"
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	c.HandleHTTP(recorder, request)
	return recorder
}

func TestADeliveryNobodySignedIsRefused(t *testing.T) {
	// The whole point of the secret: without this, anybody who learns the path
	// can post a payment event.
	c := listening(t, &InboundConfig{
		Path:               "/webhooks/stripe",
		Secret:             "whsec_the_secret",
		SignatureHeader:    "X-Signature",
		SignatureAlgorithm: "hmac-sha256",
	})

	for name, headers := range map[string]map[string]string{
		"with no signature at all": {},
		"with somebody else's":     {"X-Signature": signed("another-secret", []byte(`{"id":"evt_1"}`))},
		"with nonsense":            {"X-Signature": "not-a-signature"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := deliver(c, `{"id":"evt_1"}`, headers)
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want the delivery refused", recorder.Code)
			}
		})
	}
}

func TestADeliveryTheProviderSignedIsAccepted(t *testing.T) {
	c := listening(t, &InboundConfig{
		Path:               "/webhooks/stripe",
		Secret:             "whsec_the_secret",
		SignatureHeader:    "X-Signature",
		SignatureAlgorithm: "hmac-sha256",
	})

	body := `{"id":"evt_1","type":"payment_intent.succeeded"}`
	recorder := deliver(c, body, map[string]string{
		"X-Signature": signed("whsec_the_secret", []byte(body)),
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}

	// The provider is told what it delivered, which is how it stops retrying.
	var answer map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %v", err)
	}
	if answer["received"] != true || answer["id"] == "" {
		t.Errorf("answer = %v", answer)
	}
}

func TestASignedDeliveryReachesTheFlowParsed(t *testing.T) {
	// A handler reads the payload rather than the bytes, and the event type is
	// what a flow routes on.
	var seen *WebhookEvent
	c := listening(t, &InboundConfig{
		Path:               "/webhooks/stripe",
		Secret:             "whsec_the_secret",
		SignatureHeader:    "X-Signature",
		SignatureAlgorithm: "hmac-sha256",
	})
	c.SetHandler(func(_ context.Context, event *WebhookEvent) error {
		seen = event
		return nil
	})

	body := `{"id":"evt_1","type":"payment_intent.succeeded","data":{"amount":4200}}`
	recorder := deliver(c, body, map[string]string{
		"X-Signature": signed("whsec_the_secret", []byte(body)),
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	if seen == nil {
		t.Fatal("the delivery never reached the flow")
	}
	if !seen.SignatureValid {
		t.Error("the event does not say the signature was checked")
	}
	if seen.Payload == nil || seen.Payload["id"] != "evt_1" {
		t.Errorf("payload = %v", seen.Payload)
	}
	// Taken from the body when no header carries it, which is what Stripe does.
	if seen.Type != "payment_intent.succeeded" {
		t.Errorf("type = %q, want the one in the payload", seen.Type)
	}
	if seen.Source == "" {
		t.Error("the event does not say where it came from")
	}
}

func TestAFlowThatFailsTellsTheProviderToRetry(t *testing.T) {
	// A 200 means the provider stops trying, so answering 200 for a delivery
	// the service could not process loses the event.
	c := listening(t, &InboundConfig{Path: "/webhooks/stripe"})
	c.SetHandler(func(context.Context, *WebhookEvent) error {
		return fmt.Errorf("the database is down")
	})

	recorder := deliver(c, `{"id":"evt_1"}`, nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want the provider told to try again", recorder.Code)
	}
}

func TestOnlyTheMethodsAProviderUsesAreAnswered(t *testing.T) {
	// A GET that delivers an event is a link that fires one when a crawler
	// follows it.
	c := listening(t, &InboundConfig{Path: "/webhooks/stripe"})

	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPatch} {
		request := httptest.NewRequest(method, "/webhooks/stripe", nil)
		recorder := httptest.NewRecorder()
		c.HandleHTTP(recorder, request)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want the method refused", method, recorder.Code)
		}
	}
}

func TestADeliveryFromAnAddressNobodyAllowedIsRefused(t *testing.T) {
	// Belt and braces with the signature: a provider publishes its ranges, and
	// nothing outside them should reach the verification at all.
	c := listening(t, &InboundConfig{
		Path:       "/webhooks/stripe",
		AllowedIPs: []string{"198.51.100.0/24"},
	})

	recorder := deliver(c, `{"id":"evt_1"}`, nil) // comes from 203.0.113.7
	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want the address refused", recorder.Code)
	}
}

func TestADeliveryReplayedLaterIsRefused(t *testing.T) {
	// A signature stays valid for ever, so without the timestamp anybody who
	// captured one delivery can send it again whenever they like.
	c := listening(t, &InboundConfig{
		Path:               "/webhooks/stripe",
		Secret:             "whsec_the_secret",
		SignatureHeader:    "X-Signature",
		SignatureAlgorithm: "hmac-sha256",
		TimestampHeader:    "X-Timestamp",
		TimestampTolerance: 5 * time.Minute,
	})

	body := `{"id":"evt_1"}`
	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)

	recorder := deliver(c, body, map[string]string{
		"X-Signature": signed("whsec_the_secret", []byte(old+"."+body)),
		"X-Timestamp": old,
	})
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want a delivery from an hour ago refused", recorder.Code)
	}
}

func TestWithNoSecretEveryDeliveryIsTaken(t *testing.T) {
	// The development case, and one worth being explicit about: an endpoint
	// with no secret takes whatever arrives.
	var reached bool
	c := listening(t, &InboundConfig{Path: "/webhooks/stripe"})
	c.SetHandler(func(context.Context, *WebhookEvent) error {
		reached = true
		return nil
	})

	recorder := deliver(c, `{"id":"evt_1"}`, nil)
	if recorder.Code != http.StatusOK || !reached {
		t.Errorf("status = %d, reached = %v", recorder.Code, reached)
	}
}

// --- The formats the big providers actually send ----------------------------

func TestAStripeSignatureIsChecked(t *testing.T) {
	// Stripe signs "timestamp.payload" and sends both in one header, so a
	// verifier that checks the payload alone accepts a replay.
	verifier := NewStripeSignatureVerifier("whsec_the_secret")
	payload := []byte(`{"id":"evt_1"}`)
	now := strconv.FormatInt(time.Now().Unix(), 10)

	header := fmt.Sprintf("t=%s,v1=%s", now, signed("whsec_the_secret", []byte(now+"."+string(payload))))
	if err := verifier.Verify(payload, header, 5*time.Minute); err != nil {
		t.Fatalf("a delivery Stripe signed was refused: %v", err)
	}

	// Stripe sends more than one v1 while a secret is being rotated, and any
	// of them matching is enough.
	rotated := fmt.Sprintf("t=%s,v1=%s,v1=%s", now,
		signed("the-old-secret", []byte(now+"."+string(payload))),
		signed("whsec_the_secret", []byte(now+"."+string(payload))))
	if err := verifier.Verify(payload, rotated, 5*time.Minute); err != nil {
		t.Errorf("a delivery signed during a rotation was refused: %v", err)
	}

	for name, tc := range map[string]struct{ header string }{
		"nothing at all":              {""},
		"no timestamp":                {"v1=" + signed("whsec_the_secret", payload)},
		"no signature":                {"t=" + now},
		"a timestamp that is not one": {"t=whenever,v1=abc"},
		"signed by somebody else": {fmt.Sprintf("t=%s,v1=%s", now,
			signed("another-secret", []byte(now+"."+string(payload))))},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifier.Verify(payload, tc.header, 5*time.Minute); err == nil {
				t.Error("it was accepted")
			}
		})
	}

	// And one signed an hour ago, which is a captured delivery being replayed.
	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	replayed := fmt.Sprintf("t=%s,v1=%s", old, signed("whsec_the_secret", []byte(old+"."+string(payload))))
	if err := verifier.Verify(payload, replayed, 5*time.Minute); err == nil {
		t.Error("a delivery from an hour ago was accepted")
	}
}

func TestAGitHubSignatureIsChecked(t *testing.T) {
	verifier := NewGitHubSignatureVerifier("the-secret")
	payload := []byte(`{"action":"opened"}`)

	if err := verifier.Verify(payload, "sha256="+signed("the-secret", payload)); err != nil {
		t.Fatalf("a delivery GitHub signed was refused: %v", err)
	}

	for name, header := range map[string]string{
		"without the algorithm":   signed("the-secret", payload),
		"with the wrong one":      "sha1=" + signed("the-secret", payload),
		"signed by somebody else": "sha256=" + signed("another-secret", payload),
		"nothing at all":          "",
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifier.Verify(payload, header); err == nil {
				t.Error("it was accepted")
			}
		})
	}

	// A payload changed after signing, which is the tampering the signature
	// exists to catch.
	if err := verifier.Verify([]byte(`{"action":"closed"}`), "sha256="+signed("the-secret", payload)); err == nil {
		t.Error("a payload changed after it was signed was accepted")
	}
}

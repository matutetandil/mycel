package webhook

import (
	"context"

	"encoding/json"
	"github.com/matutetandil/mycel/v2/internal/connector"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Sending a webhook to somebody else's endpoint. What they receive is the
// contract: a signature they can check, an id they can dedupe on, and a service
// that keeps trying when their end is briefly down — without hammering it when
// it is refusing on purpose.

// receiver records what arrived and answers with the given statuses in turn.
type receiver struct {
	statuses []int
	calls    int32

	lastBody    []byte
	lastHeaders http.Header
}

func (r *receiver) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := int(atomic.AddInt32(&r.calls, 1))
		body := make([]byte, req.ContentLength)
		_, _ = req.Body.Read(body)
		r.lastBody = body
		r.lastHeaders = req.Header.Clone()

		status := http.StatusOK
		if n <= len(r.statuses) {
			status = r.statuses[n-1]
		} else if len(r.statuses) > 0 {
			status = r.statuses[len(r.statuses)-1]
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func fast() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:       3,
		InitialDelay:      time.Millisecond,
		MaxDelay:          2 * time.Millisecond,
		Multiplier:        2.0,
		RetryableStatuses: []int{500, 502, 503, 504},
	}
}

func TestWhatTheOtherEndReceives(t *testing.T) {
	r := &receiver{}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{
		URL:                server.URL,
		Secret:             "the-shared-secret",
		SignatureAlgorithm: "hmac-sha256",
		IncludeTimestamp:   true,
		Headers:            map[string]string{"X-Tenant": "waterworks"},
		Retry:              fast(),
	})

	resp, err := c.Send(context.Background(), &WebhookRequest{
		EventType:      "order.created",
		IdempotencyKey: "order-1",
		Payload:        map[string]interface{}{"id": "order-1", "total": 42},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !resp.Success {
		t.Errorf("response = %+v", resp)
	}

	// The signature, which is the only reason the other end can believe this
	// came from us.
	signature := r.lastHeaders.Get("X-Webhook-Signature")
	if signature == "" {
		t.Fatal("nothing was signed, so the receiver cannot tell who sent it")
	}
	timestamp := r.lastHeaders.Get("X-Webhook-Timestamp")
	if timestamp == "" {
		t.Fatal("no timestamp, so the receiver cannot refuse a replay")
	}

	verifier := NewSignatureVerifier("the-shared-secret", "hmac-sha256")
	if err := verifier.VerifyWithTimestamp(r.lastBody, signature, timestamp, 5*time.Minute); err != nil {
		t.Errorf("what we sent does not verify against the secret we signed with: %v", err)
	}

	// The id the receiver dedupes on: a retry has to carry the same one, or
	// every retry looks like a new event.
	if got := r.lastHeaders.Get("X-Webhook-ID"); got != "order-1" {
		t.Errorf("id = %q, want the idempotency key", got)
	}
	if got := r.lastHeaders.Get("X-Webhook-Event"); got != "order.created" {
		t.Errorf("event = %q", got)
	}
	if got := r.lastHeaders.Get("X-Tenant"); got != "waterworks" {
		t.Errorf("a configured header did not travel: %q", got)
	}
}

func TestAnEventWithNoKeyStillCarriesAnId(t *testing.T) {
	// The receiver dedupes on it, so sending nothing makes every delivery
	// unrecognisable.
	r := &receiver{}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})
	if _, err := c.Send(context.Background(), &WebhookRequest{
		Payload: map[string]interface{}{"id": "1"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if r.lastHeaders.Get("X-Webhook-ID") == "" {
		t.Error("the delivery carries no id at all")
	}
}

func TestAReceiverThatIsBrieflyDownIsTriedAgain(t *testing.T) {
	// Their deploy should not lose our event.
	r := &receiver{statuses: []int{503, 200}}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})

	resp, err := c.Send(context.Background(), &WebhookRequest{Payload: map[string]interface{}{"id": "1"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !resp.Success {
		t.Errorf("response = %+v", resp)
	}
	if atomic.LoadInt32(&r.calls) != 2 {
		t.Errorf("%d attempts, want the retry", r.calls)
	}
	if resp.Attempts != 2 {
		t.Errorf("the response says %d attempts", resp.Attempts)
	}
}

func TestAReceiverRefusingOnPurposeIsNotHammered(t *testing.T) {
	// A 400 means the payload is wrong, and sending it again produces the same
	// 400 — retrying spends the budget on something that cannot succeed.
	r := &receiver{statuses: []int{400}}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})

	resp, _ := c.Send(context.Background(), &WebhookRequest{Payload: map[string]interface{}{"id": "1"}})
	if atomic.LoadInt32(&r.calls) != 1 {
		t.Errorf("%d attempts, want no retry for a refusal", r.calls)
	}
	if resp != nil && resp.Success {
		t.Error("a refusal was read as success")
	}
}

func TestAReceiverThatStaysDownGivesUp(t *testing.T) {
	// Bounded, or a queue of events piles up behind one endpoint.
	r := &receiver{statuses: []int{503}}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})

	_, err := c.Send(context.Background(), &WebhookRequest{Payload: map[string]interface{}{"id": "1"}})
	if err == nil {
		t.Error("a receiver that never answered was reported as delivered")
	}
	if got := atomic.LoadInt32(&r.calls); got != 3 {
		t.Errorf("%d attempts, want the three configured", got)
	}
}

func TestSendingWithNowhereToSendIsRefused(t *testing.T) {
	c := NewOutboundConnector("notify", &OutboundConfig{Retry: fast()})

	if _, err := c.Send(context.Background(), &WebhookRequest{
		Payload: map[string]interface{}{"id": "1"},
	}); err == nil {
		t.Error("a webhook was sent with no address")
	}
}

func TestAPerRequestAddressOverridesTheConfiguredOne(t *testing.T) {
	// One connector serving several receivers, which is how a fan-out of
	// notifications is written.
	configured := &receiver{}
	configuredServer := configured.serve(t)
	other := &receiver{}
	otherServer := other.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: configuredServer.URL, Retry: fast()})

	if _, err := c.Send(context.Background(), &WebhookRequest{
		URL:     otherServer.URL,
		Payload: map[string]interface{}{"id": "1"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if atomic.LoadInt32(&other.calls) != 1 {
		t.Error("the address on the request was not the one used")
	}
	if atomic.LoadInt32(&configured.calls) != 0 {
		t.Error("the configured address was called as well")
	}
}

func TestAWriteIsAWebhook(t *testing.T) {
	// What a flow with this connector in its `to` produces.
	r := &receiver{}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "order.created",
		Payload: map[string]interface{}{"id": "order-1"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result == nil {
		t.Error("the write answered with nothing")
	}

	var sent map[string]interface{}
	if err := json.Unmarshal(r.lastBody, &sent); err != nil {
		t.Fatalf("what arrived is not JSON: %v (%s)", err, r.lastBody)
	}
	if sent["id"] != "order-1" {
		t.Errorf("what arrived = %v", sent)
	}
}

func TestTheDefaultsAnOutboundConnectorGets(t *testing.T) {
	// Written with nothing but an address, which is the ordinary case.
	c := NewOutboundConnector("notify", &OutboundConfig{URL: "https://example.com/hooks"})

	if c.config.Method != http.MethodPost {
		t.Errorf("method = %q", c.config.Method)
	}
	if c.config.Timeout == 0 {
		t.Error("no timeout, so a receiver that never answers holds the flow for ever")
	}
	if c.config.Retry == nil || c.config.Retry.MaxAttempts < 1 {
		t.Errorf("retry = %+v, want at least one attempt", c.config.Retry)
	}
	if c.config.SignatureHeader == "" {
		t.Error("no signature header, so a configured secret would sign into nowhere")
	}
	if c.Name() != "notify" || c.Type() != "webhook" {
		t.Errorf("name = %q, type = %q", c.Name(), c.Type())
	}
	// Nothing to connect to: a webhook is a request, not a session.
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
}

func TestATimestampedSignatureIsNotThePlainOne(t *testing.T) {
	// Signing the payload alone means a captured delivery stays valid for
	// ever, which is what the timestamp is there to stop.
	verifier := NewSignatureVerifier("the-secret", "hmac-sha256")
	payload := []byte(`{"id":"1"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	if verifier.Sign(payload) == verifier.SignWithTimestamp(payload, timestamp) {
		t.Error("the timestamp does not change the signature, so it protects nothing")
	}
}

func TestAWriteToAReceiverThatRefusedIsNotASuccessfulWrite(t *testing.T) {
	// This is what the failure above means to a flow: a webhook connector in a
	// `to` block, a receiver refusing every attempt, and a write that came back
	// with no error at all — so the message was acked, the notification never
	// arrived, and nothing retried it.
	r := &receiver{statuses: []int{503}}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})

	_, err := c.Write(context.Background(), &connector.Data{
		Target:  "order.created",
		Payload: map[string]interface{}{"id": "order-1"},
	})
	if err == nil {
		t.Fatal("a write to a receiver that refused every attempt reported success")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %q, want it to carry what the receiver answered", err)
	}
}

func TestAReceiverAnsweringSlowlyButSuccessfullyIsASuccess(t *testing.T) {
	// The other direction of the same change: nothing that did get through
	// should start failing.
	r := &receiver{statuses: []int{503, 503, 200}}
	server := r.serve(t)

	c := NewOutboundConnector("notify", &OutboundConfig{URL: server.URL, Retry: fast()})

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "order.created",
		Payload: map[string]interface{}{"id": "order-1"},
	})
	if err != nil {
		t.Fatalf("a webhook that got through on the third attempt failed: %v", err)
	}
	if result == nil || result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}
}

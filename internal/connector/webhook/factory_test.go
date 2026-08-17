package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Turning a webhook block into a webhook.
//
// The same connector type does two opposite jobs — receiving a webhook from
// somebody else, and sending one — and which it does comes from one word.
// Everything that decides whether a delivery is verified, retried or refused
// is read here, and a setting dropped on the way through is a security control
// that silently does not apply.

func build(t *testing.T, props map[string]interface{}) connector.Connector {
	t.Helper()
	built, err := (&Factory{}).Create(context.Background(), &connector.Config{Name: "hooks", Properties: props})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return built
}

func TestWhichJobAWebhookConnectorDoes(t *testing.T) {
	factory := &Factory{}
	if !factory.Supports("webhook", "") || factory.Supports("http", "") {
		t.Error("the factory answers for the wrong connector type")
	}

	// Three spellings each, because this is the sort of word people write
	// from memory.
	for _, mode := range []string{"inbound", "receive", "server"} {
		if _, ok := build(t, map[string]interface{}{"mode": mode}).(*InboundConnector); !ok {
			t.Errorf("mode %q did not produce a receiver", mode)
		}
	}
	for _, mode := range []string{"outbound", "send", "client"} {
		if _, ok := build(t, map[string]interface{}{"mode": mode, "url": "https://example.test/hook"}).(*OutboundConnector); !ok {
			t.Errorf("mode %q did not produce a sender", mode)
		}
	}

	// Nothing said is a sender, which is the commoner half.
	if _, ok := build(t, map[string]interface{}{"url": "https://example.test/hook"}).(*OutboundConnector); !ok {
		t.Error("a webhook with no mode did not produce a sender")
	}

	// A word nobody recognises is refused rather than quietly made a sender:
	// a receiver written as "listen" would otherwise start a connector that
	// receives nothing, with no endpoint and no error.
	if _, err := (&Factory{}).Create(context.Background(), &connector.Config{
		Name: "hooks", Properties: map[string]interface{}{"mode": "listen"},
	}); err == nil {
		t.Error("a webhook mode nobody implements was accepted")
	}
}

func TestWhatAReceivedWebhookIsCheckedAgainst(t *testing.T) {
	// Every one of these is a control on who may deliver a webhook. Read
	// wrongly, the endpoint still works — it just stops checking.
	built := build(t, map[string]interface{}{
		"mode":                "inbound",
		"path":                "/hooks/payments",
		"secret":              "shared-secret",
		"signature_header":    "Stripe-Signature",
		"signature_algorithm": "hmac-sha256",
		"timestamp_header":    "Stripe-Timestamp",
		"timestamp_tolerance": "30s",
		"allowed_ips":         []interface{}{"203.0.113.0/24", "198.51.100.7"},
		"trusted_proxies":     []interface{}{"10.0.0.1"},
		"require_https":       "true",
	})

	receiver, ok := built.(*InboundConnector)
	if !ok {
		t.Fatalf("built %T", built)
	}
	config := receiver.config

	if receiver.Path() != "/hooks/payments" {
		t.Errorf("path = %q, and it is the address the provider was given", receiver.Path())
	}
	if config.Secret != "shared-secret" || config.SignatureHeader != "Stripe-Signature" {
		t.Errorf("signature settings = %+v", config)
	}
	// The tolerance is what stops a captured delivery being replayed hours
	// later; too wide and the signature check buys much less.
	if config.TimestampTolerance != 30*time.Second {
		t.Errorf("tolerance = %v", config.TimestampTolerance)
	}
	if len(config.AllowedIPs) != 2 || len(config.TrustedProxies) != 1 {
		t.Errorf("addresses = %v / %v", config.AllowedIPs, config.TrustedProxies)
	}
	// Written as a word, because it comes from env() — and this one decides
	// whether a webhook carrying a signed payload is accepted in the clear.
	if !config.RequireHTTPS {
		t.Error("require_https was set and read as false")
	}

	if receiver.Name() != "hooks" || receiver.Type() != "webhook" {
		t.Errorf("name/type = %s/%s", receiver.Name(), receiver.Type())
	}
	if err := receiver.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := receiver.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestAReceiverToldNothingStillHasAnAddress(t *testing.T) {
	receiver := build(t, map[string]interface{}{"mode": "inbound"}).(*InboundConnector)

	if receiver.Path() == "" {
		t.Error("a receiver with no path listens nowhere")
	}
	if receiver.config.SignatureHeader == "" {
		t.Error("no header to read a signature from")
	}
	if receiver.config.TimestampTolerance == 0 {
		t.Error("no tolerance, so a signed delivery of any age is accepted")
	}
	// An unparseable duration falls back rather than becoming zero, which
	// would refuse every delivery.
	loose := build(t, map[string]interface{}{"mode": "inbound", "timestamp_tolerance": "whenever"}).(*InboundConnector)
	if loose.config.TimestampTolerance == 0 {
		t.Error("an unparseable tolerance became none at all")
	}
}

func TestHowASentWebhookIsRetried(t *testing.T) {
	// The retry settings are the difference between a delivery that survives
	// a receiver's bad minute and one that is lost. Every one of these was
	// unreachable until the parser learned the names.
	built := build(t, map[string]interface{}{
		"mode":              "outbound",
		"url":               "https://example.test/hook",
		"method":            "PUT",
		"secret":            "shared-secret",
		"signature_header":  "X-Signature",
		"include_timestamp": true,
		"timeout":           "45s",
		"headers":           map[string]interface{}{"X-Tenant": "acme", "X-Ignored": 42},
		"retry": map[string]interface{}{
			"attempts":           5,
			"delay":              "2s",
			"max_delay":          "1m",
			"multiplier":         2,
			"retryable_statuses": []interface{}{429, 502, 503},
		},
	})

	sender, ok := built.(*OutboundConnector)
	if !ok {
		t.Fatalf("built %T", built)
	}
	config := sender.config

	if config.URL != "https://example.test/hook" || config.Method != "PUT" {
		t.Errorf("target = %s %s", config.Method, config.URL)
	}
	if config.Timeout != 45*time.Second {
		t.Errorf("timeout = %v", config.Timeout)
	}
	if config.Headers["X-Tenant"] != "acme" {
		t.Errorf("headers = %v", config.Headers)
	}
	// A header whose value is not text is left out rather than rendered as
	// Go's idea of a number.
	if _, present := config.Headers["X-Ignored"]; present {
		t.Errorf("a header that is not text was sent: %v", config.Headers)
	}

	if config.Retry.MaxAttempts != 5 {
		t.Errorf("attempts = %d", config.Retry.MaxAttempts)
	}
	if config.Retry.InitialDelay != 2*time.Second || config.Retry.MaxDelay != time.Minute {
		t.Errorf("delays = %v / %v", config.Retry.InitialDelay, config.Retry.MaxDelay)
	}
	// A whole number in HCL arrives as an integer, so a multiplier read only
	// as a float would be dropped and the backoff would never grow.
	if config.Retry.Multiplier != 2 {
		t.Errorf("multiplier = %v, want the one written", config.Retry.Multiplier)
	}
	if len(config.Retry.RetryableStatuses) != 3 {
		t.Errorf("retryable statuses = %v", config.Retry.RetryableStatuses)
	}
}

func TestASenderToldOnlyWhereToSend(t *testing.T) {
	sender := build(t, map[string]interface{}{"url": "https://example.test/hook"}).(*OutboundConnector)

	if sender.config.Method == "" {
		t.Error("no method, so the request has none")
	}
	if sender.config.Timeout == 0 {
		t.Error("no timeout, so a receiver that never answers holds the flow")
	}
	if sender.config.Retry.MaxAttempts == 0 {
		t.Error("no attempts at all, so a receiver's bad minute loses the delivery")
	}

	// An unparseable timeout or delay falls back rather than becoming zero.
	odd := build(t, map[string]interface{}{
		"url":     "https://example.test/hook",
		"timeout": "whenever",
		"retry":   map[string]interface{}{"delay": "soon", "max_delay": "later"},
	}).(*OutboundConnector)
	if odd.config.Timeout == 0 || odd.config.Retry.InitialDelay == 0 {
		t.Errorf("unparseable settings became zero: %+v", odd.config)
	}
}

func TestAskingWhetherTheReceiverIsThere(t *testing.T) {
	// A HEAD request: any answer at all means somebody is listening. Even a
	// 405 counts, because a webhook endpoint that only takes POST is normal.
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Method
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	sender := build(t, map[string]interface{}{"url": server.URL}).(*OutboundConnector)

	if err := sender.Health(context.Background()); err != nil {
		t.Errorf("a receiver that answered was reported unhealthy: %v", err)
	}
	if asked != http.MethodHead {
		t.Errorf("asked with %s, want a request that changes nothing", asked)
	}

	// Nobody there at all is a failure.
	unreachable := build(t, map[string]interface{}{"url": "http://127.0.0.1:1", "timeout": "1s"}).(*OutboundConnector)
	if err := unreachable.Health(context.Background()); err == nil {
		t.Error("a receiver nobody is running was reported healthy")
	}

	// And a sender with nowhere to send is not called unhealthy: the address
	// may be given per message.
	if err := build(t, map[string]interface{}{}).(*OutboundConnector).Health(context.Background()); err != nil {
		t.Errorf("a sender with no fixed address was reported unhealthy: %v", err)
	}

	if err := sender.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSeveralWebhooksBehindOneServer(t *testing.T) {
	// A service receiving from more than one provider: each gets its own
	// path, its own secret, and its own flow.
	payments := NewInboundConnector("payments", &InboundConfig{Path: "/hooks/payments"})
	shipping := NewInboundConnector("shipping", &InboundConfig{Path: "/hooks/shipping"})

	server := NewWebhookServer()
	server.Register(payments)
	server.Register(shipping)

	if found, ok := server.Get("payments"); !ok || found != payments {
		t.Errorf("the payments webhook is not registered: %v, %v", found, ok)
	}
	if _, ok := server.Get("nothing"); ok {
		t.Error("a webhook nobody registered came back")
	}

	// Each path reaches its own connector, and a path nobody registered is a
	// 404 rather than the wrong flow.
	handler := server.Handler()
	if handler == nil {
		t.Fatal("the server has no handler")
	}

	for path, want := range map[string]int{
		"/hooks/payments": http.StatusOK,
		"/hooks/shipping": http.StatusOK,
		"/hooks/unknown":  http.StatusNotFound,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"id":"event-1"}`))
		handler.ServeHTTP(recorder, request)
		if want == http.StatusNotFound && recorder.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want nothing to be listening", path, recorder.Code)
		}
		if want == http.StatusOK && recorder.Code == http.StatusNotFound {
			t.Errorf("%s answered 404, and a connector is registered on it", path)
		}
	}

	// Shutting down before anything was started is what a service that fails
	// to start does on its way out.
	if err := server.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestWaitingForTheNextDelivery(t *testing.T) {
	// The reading side: a flow waits, and a delivery wakes it.
	receiver := NewInboundConnector("hooks", &InboundConfig{Path: "/hooks"})

	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/hooks", strings.NewReader(`{"id":"event-1"}`))
		receiver.HandleHTTP(recorder, request)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	event, err := receiver.Read(ctx, "")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if event == nil {
		t.Fatal("nothing came back")
	}

	// A flow shutting down while nothing has arrived stops waiting rather
	// than holding the shutdown open.
	stopped, cancelStopped := context.WithCancel(context.Background())
	cancelStopped()
	if _, err := receiver.Read(stopped, ""); err == nil {
		t.Error("a reader kept waiting after the service was told to stop")
	}
}

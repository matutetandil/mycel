package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSignatureVerifier_Sign(t *testing.T) {
	tests := []struct {
		name      string
		secret    string
		algorithm string
		payload   string
		wantLen   int
	}{
		{
			name:      "HMAC-SHA256",
			secret:    "test-secret",
			algorithm: "hmac-sha256",
			payload:   `{"event":"test"}`,
			wantLen:   64, // SHA256 hex = 64 chars
		},
		{
			name:      "HMAC-SHA1",
			secret:    "test-secret",
			algorithm: "hmac-sha1",
			payload:   `{"event":"test"}`,
			wantLen:   40, // SHA1 hex = 40 chars
		},
		{
			name:      "No algorithm",
			secret:    "test-secret",
			algorithm: "none",
			payload:   `{"event":"test"}`,
			wantLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewSignatureVerifier(tt.secret, tt.algorithm)
			sig := v.Sign([]byte(tt.payload))

			if len(sig) != tt.wantLen {
				t.Errorf("Sign() len = %d, want %d", len(sig), tt.wantLen)
			}
		})
	}
}

func TestSignatureVerifier_Verify(t *testing.T) {
	secret := "my-secret-key"
	payload := []byte(`{"event":"user.created","user_id":"123"}`)

	v := NewSignatureVerifier(secret, "hmac-sha256")
	signature := v.Sign(payload)

	tests := []struct {
		name      string
		signature string
		want      bool
	}{
		{
			name:      "Valid signature",
			signature: signature,
			want:      true,
		},
		{
			name:      "Invalid signature",
			signature: "invalid",
			want:      false,
		},
		{
			name:      "GitHub style prefix",
			signature: "sha256=" + signature,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := v.Verify(payload, tt.signature); got != tt.want {
				t.Errorf("Verify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSignatureVerifier_VerifyWithTimestamp(t *testing.T) {
	secret := "my-secret-key"
	payload := []byte(`{"event":"test"}`)

	v := NewSignatureVerifier(secret, "hmac-sha256")

	// Generate valid timestamp and signature
	timestamp := time.Now().Unix()
	timestampStr := string(rune(timestamp))

	t.Run("Valid timestamp", func(t *testing.T) {
		ts := time.Now().Format(time.RFC3339)
		signedPayload := ts + "." + string(payload)
		sig := v.Sign([]byte(signedPayload))

		err := v.VerifyWithTimestamp(payload, sig, ts, 5*time.Minute)
		if err != nil {
			t.Errorf("VerifyWithTimestamp() error = %v", err)
		}
	})

	t.Run("Expired timestamp", func(t *testing.T) {
		ts := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
		signedPayload := ts + "." + string(payload)
		sig := v.Sign([]byte(signedPayload))

		err := v.VerifyWithTimestamp(payload, sig, ts, 5*time.Minute)
		if err == nil {
			t.Error("VerifyWithTimestamp() should fail for expired timestamp")
		}
	})

	_ = timestampStr // silence unused variable warning
}

func TestGitHubSignatureVerifier(t *testing.T) {
	secret := "github-webhook-secret"
	payload := []byte(`{"action":"push","ref":"refs/heads/main"}`)

	v := NewGitHubSignatureVerifier(secret)

	// Generate valid signature
	sv := NewSignatureVerifier(secret, "hmac-sha256")
	sig := "sha256=" + sv.Sign(payload)

	t.Run("Valid GitHub signature", func(t *testing.T) {
		err := v.Verify(payload, sig)
		if err != nil {
			t.Errorf("Verify() error = %v", err)
		}
	})

	t.Run("Invalid GitHub signature", func(t *testing.T) {
		err := v.Verify(payload, "sha256=invalid")
		if err == nil {
			t.Error("Verify() should fail for invalid signature")
		}
	})

	t.Run("Missing prefix", func(t *testing.T) {
		err := v.Verify(payload, sv.Sign(payload))
		if err == nil {
			t.Error("Verify() should fail without sha256= prefix")
		}
	})
}

func TestOutboundConnector_Send(t *testing.T) {
	// Create test server
	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	config := &OutboundConfig{
		URL:                server.URL,
		Method:             "POST",
		Secret:             "test-secret",
		SignatureAlgorithm: "hmac-sha256",
		SignatureHeader:    "X-Webhook-Signature",
		IncludeTimestamp:   true,
		Timeout:            5 * time.Second,
		Retry:              DefaultRetryConfig(),
	}

	c := NewOutboundConnector("test", config)

	t.Run("Successful send", func(t *testing.T) {
		req := &WebhookRequest{
			Payload:   map[string]string{"event": "user.created"},
			EventType: "user.created",
		}

		resp, err := c.Send(context.Background(), req)
		if err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if !resp.Success {
			t.Errorf("Send() success = false, want true")
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Send() statusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		// Check headers
		if receivedHeaders.Get("Content-Type") != "application/json" {
			t.Error("Missing Content-Type header")
		}
		if receivedHeaders.Get("X-Webhook-Signature") == "" {
			t.Error("Missing signature header")
		}
		if receivedHeaders.Get("X-Webhook-Timestamp") == "" {
			t.Error("Missing timestamp header")
		}
		if receivedHeaders.Get("X-Webhook-Event") != "user.created" {
			t.Error("Missing or wrong event header")
		}

		// Check body
		var body map[string]string
		if err := json.Unmarshal(receivedBody, &body); err != nil {
			t.Fatalf("Failed to unmarshal body: %v", err)
		}
		if body["event"] != "user.created" {
			t.Errorf("Body event = %s, want user.created", body["event"])
		}
	})
}

func TestOutboundConnector_Retry(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &OutboundConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
		Retry: &RetryConfig{
			MaxAttempts:       3,
			InitialDelay:      10 * time.Millisecond,
			MaxDelay:          100 * time.Millisecond,
			Multiplier:        2.0,
			RetryableStatuses: []int{503},
		},
	}

	c := NewOutboundConnector("test", config)

	resp, err := c.Send(context.Background(), &WebhookRequest{
		Payload: map[string]string{"test": "data"},
	})

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if !resp.Success {
		t.Error("Send() should succeed after retries")
	}

	if resp.Attempts != 3 {
		t.Errorf("Send() attempts = %d, want 3", resp.Attempts)
	}

	if attempts != 3 {
		t.Errorf("Server received %d requests, want 3", attempts)
	}
}

func TestInboundConnector_HandleHTTP(t *testing.T) {
	config := &InboundConfig{
		Path:               "/webhook",
		Secret:             "test-secret",
		SignatureAlgorithm: "hmac-sha256",
		SignatureHeader:    "X-Webhook-Signature",
		TimestampTolerance: 5 * time.Minute,
	}

	c := NewInboundConnector("test", config)

	payload := []byte(`{"event":"user.created","user_id":"123"}`)
	verifier := NewSignatureVerifier(config.Secret, config.SignatureAlgorithm)
	signature := verifier.Sign(payload)

	t.Run("Valid webhook", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signature)
		req.Header.Set("X-Webhook-Event", "user.created")

		w := httptest.NewRecorder()
		c.HandleHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("HandleHTTP() status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if resp["received"] != true {
			t.Error("Response should have received=true")
		}
	})

	t.Run("Invalid signature", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", "invalid-signature")

		w := httptest.NewRecorder()
		c.HandleHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("HandleHTTP() status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("Wrong method", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/webhook", nil)

		w := httptest.NewRecorder()
		c.HandleHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("HandleHTTP() status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestInboundConnector_EventExtraction(t *testing.T) {
	config := DefaultInboundConfig()
	config.Secret = "" // Disable signature verification for this test
	c := NewInboundConnector("test", config)

	tests := []struct {
		name      string
		headers   map[string]string
		payload   string
		wantEvent string
	}{
		{
			name:      "GitHub event header",
			headers:   map[string]string{"X-GitHub-Event": "push"},
			payload:   `{}`,
			wantEvent: "push",
		},
		{
			name:      "Stripe event header",
			headers:   map[string]string{"X-Stripe-Event": "charge.succeeded"},
			payload:   `{}`,
			wantEvent: "charge.succeeded",
		},
		{
			name:      "Event in payload",
			headers:   map[string]string{},
			payload:   `{"type":"order.created"}`,
			wantEvent: "order.created",
		},
		{
			name:      "Event in payload (event field)",
			headers:   map[string]string{},
			payload:   `{"event":"payment.received"}`,
			wantEvent: "payment.received",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhook", strings.NewReader(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			w := httptest.NewRecorder()
			c.HandleHTTP(w, req)

			// Get event from channel
			select {
			case event := <-c.Events():
				if event.Type != tt.wantEvent {
					t.Errorf("Event type = %s, want %s", event.Type, tt.wantEvent)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("Timeout waiting for event")
			}
		})
	}
}

func TestFactory_Create(t *testing.T) {
	f := NewFactory()

	t.Run("Create outbound", func(t *testing.T) {
		config := map[string]interface{}{
			"mode":   "outbound",
			"url":    "https://example.com/webhook",
			"secret": "test-secret",
		}

		c, err := f.Create("test", config)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if c.Type() != "webhook" {
			t.Errorf("Type() = %s, want webhook", c.Type())
		}

		oc, ok := c.(*OutboundConnector)
		if !ok {
			t.Fatal("Expected OutboundConnector")
		}
		if oc.config.URL != "https://example.com/webhook" {
			t.Errorf("URL = %s, want https://example.com/webhook", oc.config.URL)
		}
	})

	t.Run("Create inbound", func(t *testing.T) {
		config := map[string]interface{}{
			"mode":   "inbound",
			"path":   "/webhooks/stripe",
			"secret": "stripe-secret",
		}

		c, err := f.Create("test", config)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		ic, ok := c.(*InboundConnector)
		if !ok {
			t.Fatal("Expected InboundConnector")
		}
		if ic.config.Path != "/webhooks/stripe" {
			t.Errorf("Path = %s, want /webhooks/stripe", ic.config.Path)
		}
	})
}

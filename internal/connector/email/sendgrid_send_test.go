package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Sending mail through SendGrid, which is what a service uses when it has no
// SMTP to speak to. What matters is that a refusal is a failure — a flow that
// reads "sent" for mail nobody received will never send it again.

type sendgridAPI struct {
	status   int
	calls    int32
	lastBody map[string]interface{}
	lastAuth string
}

func (s *sendgridAPI) serve(t *testing.T) *SendGridConnector {
	t.Helper()
	if s.status == 0 {
		s.status = http.StatusAccepted
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.calls, 1)
		s.lastAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&s.lastBody)

		if s.status == http.StatusAccepted {
			w.Header().Set("X-Message-Id", "sg-message-1")
		}
		w.WriteHeader(s.status)
		if s.status >= 400 {
			_, _ = w.Write([]byte(`{"errors":[{"message":"the sender is not verified"}]}`))
		}
	}))
	t.Cleanup(server.Close)

	return NewSendGridConnector("mail", &Config{
		Driver:   "sendgrid",
		From:     "orders@example.com",
		FromName: "Orders",
		SendGrid: &SendGridConfig{APIKey: "SG.the-key", Endpoint: server.URL},
	})
}

func TestMailSentThroughSendGridCarriesWhatItWasGiven(t *testing.T) {
	api := &sendgridAPI{}
	c := api.serve(t)

	result, err := c.Send(context.Background(), &Email{
		To:       []Recipient{{Email: "ada@example.com"}},
		Subject:  "Your order",
		HTMLBody: "<p>On its way</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success || result.MessageID != "sg-message-1" {
		t.Errorf("result = %+v, want the id SendGrid issued", result)
	}

	// The key travels as a bearer token; without it every send is refused with
	// something that names neither the service nor the message.
	if api.lastAuth != "Bearer SG.the-key" {
		t.Errorf("authorization = %q", api.lastAuth)
	}

	// And the message itself, in the shape SendGrid takes.
	from, ok := api.lastBody["from"].(map[string]interface{})
	if !ok || from["email"] != "orders@example.com" {
		t.Errorf("from = %v, want the configured sender", api.lastBody["from"])
	}
	personalizations, ok := api.lastBody["personalizations"].([]interface{})
	if !ok || len(personalizations) == 0 {
		t.Fatalf("personalizations = %v", api.lastBody["personalizations"])
	}
	if api.lastBody["subject"] != "Your order" {
		t.Errorf("subject = %v", api.lastBody["subject"])
	}
}

func TestMailSendGridRefusedIsAFailure(t *testing.T) {
	// An unverified sender, a key that was rotated, a quota — all of them come
	// back as a status, and a flow reading them as success never sends the
	// mail again.
	api := &sendgridAPI{status: http.StatusForbidden}
	c := api.serve(t)

	result, err := c.Send(context.Background(), &Email{
		To: []Recipient{{Email: "ada@example.com"}}, Subject: "Your order", HTMLBody: "<p>Hello</p>",
	})
	if err == nil {
		t.Fatal("mail SendGrid refused was reported as sent")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v", result)
	}
	if !strings.Contains(result.Error, "not verified") {
		t.Errorf("the reason SendGrid gave was lost: %q", result.Error)
	}
}

func TestAWriteThatCouldNotBeSentFailsTheFlow(t *testing.T) {
	// The path a flow takes: a `to` block with an email connector in it.
	api := &sendgridAPI{status: http.StatusInternalServerError}
	c := api.serve(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Target: "ada@example.com",
		Payload: map[string]interface{}{
			"subject": "Your order",
			"html":    "<p>Hello</p>",
		},
	})
	if err == nil {
		t.Error("a write that never reached anybody reported success")
	}
}

func TestAWriteBecomesTheMessage(t *testing.T) {
	api := &sendgridAPI{}
	c := api.serve(t)

	result, err := c.Write(context.Background(), &connector.Data{
		Target: "ada@example.com",
		Payload: map[string]interface{}{
			"subject": "Your order",
			"html":    "<p>On its way</p>",
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}
	if api.lastBody["subject"] != "Your order" {
		t.Errorf("subject = %v, want the one in the payload", api.lastBody["subject"])
	}
}

func TestAMessageWithNoRecipientIsRefusedBeforeItIsSent(t *testing.T) {
	// Rather than spending a call to the provider to be told.
	api := &sendgridAPI{}
	c := api.serve(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"subject": "Your order"},
	})
	if err == nil {
		t.Fatal("a message with nobody to send it to was accepted")
	}
	if atomic.LoadInt32(&api.calls) != 0 {
		t.Error("the provider was called for a message that could not be sent")
	}
}

func TestHealthAsksTheProviderSomethingItCanAnswer(t *testing.T) {
	api := &sendgridAPI{status: http.StatusOK}
	c := api.serve(t)

	if err := c.Health(context.Background()); err != nil {
		t.Errorf("a provider that is answering was reported unhealthy: %v", err)
	}
	if atomic.LoadInt32(&api.calls) == 0 {
		t.Error("health answered without asking the provider anything")
	}

	// And a key the provider refuses is not healthy, since nothing will send.
	refusing := &sendgridAPI{status: http.StatusUnauthorized}
	if err := refusing.serve(t).Health(context.Background()); err == nil {
		t.Error("a provider refusing the key was reported healthy")
	}
}

func TestAProviderThatCannotBeReachedIsNotHealthy(t *testing.T) {
	c := NewSendGridConnector("mail", &Config{
		Driver:   "sendgrid",
		From:     "orders@example.com",
		SendGrid: &SendGridConfig{APIKey: "SG.the-key", Endpoint: "http://127.0.0.1:1"},
	})

	if err := c.Health(context.Background()); err == nil {
		t.Error("a provider nobody is running was reported healthy")
	}
	if _, err := c.Send(context.Background(), &Email{
		To: []Recipient{{Email: "ada@example.com"}}, Subject: "Hello", HTMLBody: "<p>Hello</p>",
	}); err == nil {
		t.Error("mail was sent to a provider nobody is running")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

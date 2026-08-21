package email

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Mail somebody actually reads is HTML built from a template, and the template
// is a file on disk rather than a string in the configuration. Nothing
// exercised it: a template that does not render is a message sent with an empty
// body, which is worse than one that fails to send.

func templateFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "order.html")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the template: %v", err)
	}
	return path
}

func TestATemplateBecomesTheMessageBody(t *testing.T) {
	path := templateFile(t, `<p>Hello {{.name}}, your order {{.reference}} is on its way.</p>`)

	email := &Email{
		To:           []Recipient{{Email: "ada@example.com"}},
		Subject:      "Your order",
		Template:     path,
		TemplateData: map[string]interface{}{"name": "Ada", "reference": "ORD-1"},
	}

	if err := email.RenderTemplate(nil); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if !strings.Contains(email.HTMLBody, "Hello Ada") || !strings.Contains(email.HTMLBody, "ORD-1") {
		t.Errorf("body = %q, want the values filled in", email.HTMLBody)
	}
}

func TestATemplateWithNoDataOfItsOwnUsesThePayload(t *testing.T) {
	// The ordinary case: a flow writes the payload and the template names its
	// fields, without repeating them under template_data.
	path := templateFile(t, `<p>Order {{.reference}}</p>`)

	email := &Email{To: []Recipient{{Email: "ada@example.com"}}, Template: path}
	if err := email.RenderTemplate(map[string]interface{}{"reference": "ORD-1"}); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if !strings.Contains(email.HTMLBody, "ORD-1") {
		t.Errorf("body = %q", email.HTMLBody)
	}
}

func TestATemplateThatCannotBeUsedStopsTheMessage(t *testing.T) {
	// Each of these would otherwise send mail with an empty body, and nobody
	// notices an empty body until a customer says so.
	for name, template := range map[string]string{
		"a file that is not there": filepath.Join(t.TempDir(), "absent.html"),
		"one that does not parse":  templateFile(t, `<p>{{.name</p>`),
	} {
		t.Run(name, func(t *testing.T) {
			email := &Email{To: []Recipient{{Email: "ada@example.com"}}, Template: template}
			if err := email.RenderTemplate(nil); err == nil {
				t.Error("the message was built anyway")
			}
		})
	}
}

func TestNoTemplateLeavesTheBodyAlone(t *testing.T) {
	// A flow that writes its own HTML must not have it replaced by nothing.
	email := &Email{
		To:       []Recipient{{Email: "ada@example.com"}},
		HTMLBody: "<p>Written by the flow</p>",
	}
	if err := email.RenderTemplate(nil); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if email.HTMLBody != "<p>Written by the flow</p>" {
		t.Errorf("body = %q, want it untouched", email.HTMLBody)
	}
}

func TestAConnectorsTemplateAppliesToEveryMessage(t *testing.T) {
	// Configured once on the connector rather than repeated in every flow —
	// and a message naming its own still wins.
	path := templateFile(t, `<p>From the connector: {{.reference}}</p>`)
	api := &sendgridAPI{}
	c := api.serve(t)
	c.config.Template = path

	if _, err := c.Send(context.Background(), &Email{
		To:           []Recipient{{Email: "ada@example.com"}},
		Subject:      "Your order",
		TemplateData: map[string]interface{}{"reference": "ORD-1"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	content, ok := api.lastBody["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("content = %v", api.lastBody["content"])
	}
	part, _ := content[0].(map[string]interface{})
	if body, _ := part["value"].(string); !strings.Contains(body, "From the connector") {
		t.Errorf("the connector's template was not applied: %v", part)
	}
}

func TestAMessageBuiltForSMTPCarriesEveryPart(t *testing.T) {
	// The wire format an SMTP server receives. A header that is missing or in
	// the wrong place is a message a client renders as source.
	c := NewSMTPConnector("mail", &Config{
		Driver:   "smtp",
		From:     "orders@example.com",
		FromName: "Orders",
		SMTP:     &SMTPConfig{Host: "localhost", Port: 25},
	})

	message, err := c.buildMessage(&Email{
		To:       []Recipient{{Email: "ada@example.com", Name: "Ada"}},
		CC:       []Recipient{{Email: "grace@example.com"}},
		Subject:  "Your order",
		TextBody: "On its way",
		HTMLBody: "<p>On its way</p>",
	})
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	wire := string(message)
	for _, want := range []string{
		"From: Orders <orders@example.com>",
		"To: ",
		"ada@example.com",
		"Cc: ",
		"Subject: Your order",
		"MIME-Version: 1.0",
		"text/plain",
		"text/html",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("the message does not carry %q", want)
		}
	}

	// Attachments are deliberately not here: they are read from a payload and
	// no provider sends them, which is recorded in DISCREPANCIES and pinned by
	// TestAnAttachmentGoesNowhereAndThisIsWhereThatIsRecorded.

	// Headers end and the body begins at the blank line; without it a server
	// reads the whole thing as headers.
	if !strings.Contains(wire, "\r\n\r\n") {
		t.Error("there is no blank line between the headers and the body")
	}
}

func TestSendingWithNoSenderIsRefused(t *testing.T) {
	// Every provider refuses this, and refusing here says which flow rather
	// than which API.
	c := NewSMTPConnector("mail", &Config{
		Driver: "smtp",
		SMTP:   &SMTPConfig{Host: "127.0.0.1", Port: 1},
	})

	_, err := c.Send(context.Background(), &Email{
		To: []Recipient{{Email: "ada@example.com"}}, Subject: "Hello",
	})
	if err == nil {
		t.Error("a message with no sender was sent")
	}
}

func TestSESWithoutAClientSaysSo(t *testing.T) {
	// A connector whose credentials never resolved: every send has to report
	// that rather than crash on a nil client.
	c := NewSESConnector("mail", &Config{
		Driver: "ses",
		From:   "orders@example.com",
		SES:    &SESConfig{Region: "ap-southeast-2"},
	})

	result, err := c.Send(context.Background(), &Email{
		To: []Recipient{{Email: "ada@example.com"}}, Subject: "Hello", HTMLBody: "<p>Hello</p>",
	})
	if err == nil {
		t.Fatal("mail was sent through a connector with no client")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v", result)
	}

	// And through the flow path.
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "ada@example.com",
		Payload: map[string]interface{}{"subject": "Hello", "html": "<p>Hello</p>"},
	}); err == nil {
		t.Error("a write succeeded through a connector with no client")
	}
}

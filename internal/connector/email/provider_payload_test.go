package email

import (
	"strings"
	"testing"
	"time"
)

// Whatever a flow puts in a message has to survive the trip into the shape the
// provider wants. A field read off the payload and then left out of the request
// is invisible from inside Mycel: the send succeeds, the provider is happy, and
// the copy nobody was sent is noticed by whoever expected it.

func fullMessage() *Email {
	sendAt := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	return &Email{
		From: "billing@example.com", FromName: "Billing",
		To:      []Recipient{{Email: "ada@example.com", Name: "Ada"}},
		CC:      []Recipient{{Email: "books@example.com"}},
		BCC:     []Recipient{{Email: "audit@example.com"}},
		ReplyTo: "support@example.com", ReplyToName: "Support",
		Subject:    "Invoice 4001",
		TextBody:   "Your invoice is attached.",
		HTMLBody:   "<p>Your invoice is attached.</p>",
		Headers:    map[string]string{"X-Invoice": "4001"},
		Tags:       []string{"billing", "invoice"},
		TrackOpens: true, TrackClicks: true,
		SendAt: &sendAt,
	}
}

func sendgrid(t *testing.T) *SendGridConnector {
	t.Helper()
	return NewSendGridConnector("mail", &Config{
		From: "noreply@example.com", FromName: "Example",
		SendGrid: &SendGridConfig{APIKey: "sg-key"},
	})
}

func TestEverythingAMessageCarriesReachesSendGrid(t *testing.T) {
	payload := sendgrid(t).buildPayload(fullMessage())

	from, _ := payload["from"].(map[string]string)
	if from["email"] != "billing@example.com" || from["name"] != "Billing" {
		t.Errorf("from = %v", payload["from"])
	}

	personalizations, _ := payload["personalizations"].([]interface{})
	if len(personalizations) != 1 {
		t.Fatalf("personalizations = %v", payload["personalizations"])
	}
	people, _ := personalizations[0].(map[string]interface{})

	for _, field := range []string{"to", "cc", "bcc"} {
		if people[field] == nil {
			t.Errorf("%s was dropped, so somebody who should have had a copy did not", field)
		}
	}
	if people["subject"] != "Invoice 4001" {
		t.Errorf("subject = %v", people["subject"])
	}

	replyTo, _ := payload["reply_to"].(map[string]string)
	if replyTo["email"] != "support@example.com" {
		t.Errorf("reply_to = %v, want replies going to support rather than billing", payload["reply_to"])
	}

	content, _ := payload["content"].([]map[string]string)
	if len(content) != 2 {
		t.Fatalf("content = %v, want both the text and the html part", payload["content"])
	}

	if payload["headers"] == nil {
		t.Error("custom headers were dropped")
	}
	if payload["categories"] == nil {
		t.Error("tags were dropped, so nothing can be grouped by them at the provider")
	}
	if payload["send_at"] != int64(1772355600) {
		t.Errorf("send_at = %v, want the message scheduled rather than sent now", payload["send_at"])
	}

	tracking, _ := payload["tracking_settings"].(map[string]interface{})
	if tracking["open_tracking"] == nil || tracking["click_tracking"] == nil {
		t.Errorf("tracking = %v", payload["tracking_settings"])
	}
}

func TestAMessageWithNoSenderUsesTheConfiguredOne(t *testing.T) {
	// Almost every flow leaves the sender to the connector.
	payload := sendgrid(t).buildPayload(&Email{
		To: []Recipient{{Email: "ada@example.com"}}, Subject: "Hello",
	})

	from, _ := payload["from"].(map[string]string)
	if from["email"] != "noreply@example.com" || from["name"] != "Example" {
		t.Errorf("from = %v, want the connector's own sender", payload["from"])
	}
}

func TestATemplateIsSentInsteadOfABodyNotBesideIt(t *testing.T) {
	// SendGrid rejects a request carrying both, and the data a template needs
	// has to travel with it.
	payload := sendgrid(t).buildPayload(&Email{
		To:         []Recipient{{Email: "ada@example.com"}},
		TemplateID: "d-1", TemplateData: map[string]interface{}{"name": "Ada"},
		TextBody: "this should not be sent",
	})

	if payload["template_id"] != "d-1" {
		t.Errorf("template_id = %v", payload["template_id"])
	}
	if payload["content"] != nil {
		t.Error("a body was sent alongside a template, which the provider refuses")
	}

	personalizations, _ := payload["personalizations"].([]interface{})
	people, _ := personalizations[0].(map[string]interface{})
	if people["dynamic_template_data"] == nil {
		t.Error("the template was sent with nothing to fill it in")
	}
}

func TestEverythingAMessageCarriesReachesSES(t *testing.T) {
	connector := NewSESConnector("mail", &Config{
		From: "noreply@example.com",
		SES:  &SESConfig{Region: "us-east-1"},
	})

	input, err := connector.buildInput(fullMessage())
	if err != nil {
		t.Fatalf("buildInput: %v", err)
	}

	if *input.FromEmailAddress != "Billing <billing@example.com>" {
		t.Errorf("from = %q", *input.FromEmailAddress)
	}
	if len(input.Destination.ToAddresses) != 1 ||
		!strings.Contains(input.Destination.ToAddresses[0], "ada@example.com") {
		t.Errorf("to = %v", input.Destination.ToAddresses)
	}
	if len(input.Destination.CcAddresses) != 1 {
		t.Error("cc was dropped, so somebody who should have had a copy did not")
	}
	if len(input.Destination.BccAddresses) != 1 {
		t.Error("bcc was dropped")
	}
	if len(input.ReplyToAddresses) != 1 {
		t.Error("reply-to was dropped, so replies go to the sender")
	}
}

func TestSESRefusesAMessageWithNobodyToSendItFrom(t *testing.T) {
	connector := NewSESConnector("mail", &Config{SES: &SESConfig{Region: "us-east-1"}})

	if _, err := connector.buildInput(&Email{
		To: []Recipient{{Email: "ada@example.com"}}, Subject: "Hello",
	}); err == nil {
		t.Error("a message with no sender was built rather than reported")
	}
}

func TestAnAttachmentGoesNowhereAndThisIsWhereThatIsRecorded(t *testing.T) {
	// The Email type has attachments and the payload reader fills them in, and
	// none of the three providers send them: SMTP builds multipart/alternative
	// for text and html and never multipart/mixed, and neither provider payload
	// mentions them. Nothing offers attachments to a user — they are in no
	// schema and no documentation — so this is half a feature rather than a
	// broken promise, and it is recorded in DISCREPANCIES.md rather than
	// quietly filled in.
	//
	// The test is here so that implementing it fails this and the note gets
	// removed with the same change.
	message := fullMessage()
	message.Attachments = []Attachment{{
		Filename: "invoice.pdf", Content: []byte("%PDF-1.4"), ContentType: "application/pdf",
	}}

	payload := sendgrid(t).buildPayload(message)
	if payload["attachments"] != nil {
		t.Error("attachments are sent now: remove the note in DISCREPANCIES.md")
	}
}

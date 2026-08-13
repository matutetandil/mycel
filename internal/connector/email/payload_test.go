package email

import (
	"testing"
)

// A flow hands the email connector a map — what a transform produces, what JSON
// decodes into. Everything the message is supposed to carry has to survive that
// hand-off, and the parts that do not are the ones nobody sees go missing: the
// mail is sent, it looks right, and the copy that was meant for accounts never
// arrives, or the invoice is not attached to the invoice email.

func TestTheAddressesAMessageIsSentTo(t *testing.T) {
	email, err := emailFromData("", map[string]interface{}{
		"to":       []interface{}{"someone@example.com", "other@example.com"},
		"cc":       []interface{}{"accounts@example.com"},
		"bcc":      "archive@example.com",
		"reply_to": "support@example.com",
		"subject":  "Your invoice",
	})
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}

	if len(email.To) != 2 {
		t.Errorf("to = %v, want both", email.To)
	}
	// A copy that silently does not go is the failure this is about: somebody
	// is expecting it and nobody finds out for a month.
	if len(email.CC) != 1 || email.CC[0].Email != "accounts@example.com" {
		t.Errorf("cc = %v, want the copy that was asked for", email.CC)
	}
	if len(email.BCC) != 1 || email.BCC[0].Email != "archive@example.com" {
		t.Errorf("bcc = %v", email.BCC)
	}
	// Without this, replies go to whatever address the service sends from,
	// which is usually one nobody reads.
	if email.ReplyTo != "support@example.com" {
		t.Errorf("reply_to = %q", email.ReplyTo)
	}
}

func TestAnAttachmentReachesTheMessage(t *testing.T) {
	// Generating a PDF and emailing it is a flow this product ships an example
	// for. An invoice email with no invoice on it is worse than none.
	email, err := emailFromData("someone@example.com", map[string]interface{}{
		"subject": "Your invoice",
		"attachments": []interface{}{
			map[string]interface{}{
				"filename":     "invoice-A-1234.pdf",
				"content_type": "application/pdf",
				"content":      []byte("%PDF-1.4"),
			},
		},
	})
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}

	if len(email.Attachments) != 1 {
		t.Fatalf("attachments = %v, want the one that was attached", email.Attachments)
	}
	if email.Attachments[0].Filename != "invoice-A-1234.pdf" {
		t.Errorf("filename = %q", email.Attachments[0].Filename)
	}
	if len(email.Attachments[0].Content) == 0 {
		t.Error("the attachment has no content")
	}
}

func TestAnAttachmentCanBeNamedByAddress(t *testing.T) {
	// The other way a flow supplies one: a link the provider fetches, rather
	// than bytes carried through the flow.
	email, err := emailFromData("someone@example.com", map[string]interface{}{
		"attachments": []interface{}{
			map[string]interface{}{"filename": "report.csv", "url": "https://example.com/report.csv"},
		},
	})
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}
	if len(email.Attachments) != 1 || email.Attachments[0].URL == "" {
		t.Errorf("attachments = %v", email.Attachments)
	}
}

func TestBothSpellingsOfEachBody(t *testing.T) {
	// `text` and `text_body` were both read and only `html_body` was, so
	// somebody writing the obvious pair — text and html — sent a message with
	// no HTML in it at all.
	for name, payload := range map[string]map[string]interface{}{
		"the short spellings": {"text": "plain", "html": "<p>rich</p>"},
		"the long spellings":  {"text_body": "plain", "html_body": "<p>rich</p>"},
	} {
		t.Run(name, func(t *testing.T) {
			email, err := emailFromData("someone@example.com", payload)
			if err != nil {
				t.Fatalf("emailFromData: %v", err)
			}
			if email.TextBody != "plain" {
				t.Errorf("text = %q", email.TextBody)
			}
			if email.HTMLBody != "<p>rich</p>" {
				t.Errorf("html = %q, want the rich body that was written", email.HTMLBody)
			}
		})
	}
}

func TestTheSenderCanBeNamed(t *testing.T) {
	email, err := emailFromData("someone@example.com", map[string]interface{}{
		"from":      "orders@example.com",
		"from_name": "Example Orders",
		"subject":   "Your order",
	})
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}
	if email.From != "orders@example.com" {
		t.Errorf("from = %q", email.From)
	}
	if email.FromName != "Example Orders" {
		t.Errorf("from_name = %q, want the name a person sees instead of an address", email.FromName)
	}
}

func TestCustomHeadersAreCarried(t *testing.T) {
	// How a service threads its own identifiers through a provider — a message
	// id to reconcile a bounce against, for instance.
	email, err := emailFromData("someone@example.com", map[string]interface{}{
		"headers": map[string]interface{}{"X-Order-Id": "A-1234"},
	})
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}
	if email.Headers["X-Order-Id"] != "A-1234" {
		t.Errorf("headers = %v", email.Headers)
	}
}

func TestARecipientCanCarryAName(t *testing.T) {
	email, err := emailFromData("", map[string]interface{}{
		"to": []interface{}{
			map[string]interface{}{"email": "someone@example.com", "name": "Someone"},
		},
	})
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}
	if len(email.To) != 1 || email.To[0].Name != "Someone" {
		t.Errorf("to = %v, want the name beside the address", email.To)
	}
}

func TestTheTargetIsTheRecipientWhenTheBodyNamesNone(t *testing.T) {
	email, err := emailFromData("someone@example.com", "your order shipped")
	if err != nil {
		t.Fatalf("emailFromData: %v", err)
	}
	if len(email.To) != 1 || email.To[0].Email != "someone@example.com" {
		t.Errorf("to = %v", email.To)
	}
	if email.TextBody != "your order shipped" {
		t.Errorf("text = %q", email.TextBody)
	}
}

func TestSomethingThatIsNotAMessageIsRefused(t *testing.T) {
	if _, err := emailFromData("someone@example.com", 42); err == nil {
		t.Error("a number was accepted as an email")
	}
}

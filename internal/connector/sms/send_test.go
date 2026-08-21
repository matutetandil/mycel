package sms

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Sending a text message.
//
// A text message costs money per send and is what a one-time code arrives on,
// so the two things that matter are that a refused send is not read as
// delivered, and that the message goes out as the kind of traffic it was
// configured to be.

type twilioAPI struct {
	status int
	answer string

	form url.Values
	user string
	pass string
	path string
}

func (a *twilioAPI) serve(t *testing.T, cfg *Config) *TwilioConnector {
	t.Helper()
	if a.status == 0 {
		a.status = http.StatusCreated
	}
	if a.answer == "" {
		a.answer = `{"sid":"SM-1","status":"queued"}`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.path = r.URL.Path
		a.user, a.pass, _ = r.BasicAuth()
		body, _ := io.ReadAll(r.Body)
		a.form, _ = url.ParseQuery(string(body))
		w.WriteHeader(a.status)
		_, _ = w.Write([]byte(a.answer))
	}))
	t.Cleanup(server.Close)

	cfg.Twilio.APIURL = server.URL
	return NewTwilioConnector("sms", cfg)
}

func TestATextMessageIsSent(t *testing.T) {
	api := &twilioAPI{}
	c := api.serve(t, &Config{
		Driver: "twilio",
		Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "secret", From: "+6491234567"},
	})

	result, err := c.Send(context.Background(), &Message{To: "+6497654321", Body: "Your code is 1234"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success || result.MessageID != "SM-1" {
		t.Errorf("result = %+v", result)
	}

	// The account is part of the path as well as the credential; a message
	// posted to the wrong one is billed to somebody else.
	if api.path != "/2010-04-01/Accounts/AC-1/Messages.json" {
		t.Errorf("posted to %s", api.path)
	}
	if api.user != "AC-1" || api.pass != "secret" {
		t.Errorf("authenticated as %s/%s", api.user, api.pass)
	}
	if api.form.Get("To") != "+6497654321" || api.form.Get("Body") != "Your code is 1234" {
		t.Errorf("sent %v", api.form)
	}
	if api.form.Get("From") != "+6491234567" {
		t.Errorf("from = %q, want the configured number", api.form.Get("From"))
	}
}

func TestWhichNumberAMessageComesFrom(t *testing.T) {
	// Three places it can be set, and the wrong one means either a rejected
	// send or a reply nobody reads.
	for name, tc := range map[string]struct {
		config  *Config
		message *Message
		want    string
	}{
		"the message names it": {
			&Config{Driver: "twilio", From: "+6400000000",
				Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s", From: "+6411111111"}},
			&Message{To: "+64222", Body: "hi", From: "+6433333333"},
			"+6433333333",
		},
		"otherwise the twilio block": {
			&Config{Driver: "twilio", From: "+6400000000",
				Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s", From: "+6411111111"}},
			&Message{To: "+64222", Body: "hi"},
			"+6411111111",
		},
		"otherwise the connector default": {
			&Config{Driver: "twilio", From: "+6400000000",
				Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s"}},
			&Message{To: "+64222", Body: "hi"},
			"+6400000000",
		},
	} {
		t.Run(name, func(t *testing.T) {
			api := &twilioAPI{}
			c := api.serve(t, tc.config)
			if _, err := c.Send(context.Background(), tc.message); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if got := api.form.Get("From"); got != tc.want {
				t.Errorf("From = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAMessageTheProviderRefusedIsAFailure(t *testing.T) {
	// An unreachable number, a blocked one, a spent balance: all of them come
	// back as a 400, and all of them mean nobody got the message.
	api := &twilioAPI{
		status: http.StatusBadRequest,
		answer: `{"code":21211,"message":"The 'To' number is not a valid phone number."}`,
	}
	c := api.serve(t, &Config{Driver: "twilio",
		Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s", From: "+6491234567"}})

	result, err := c.Send(context.Background(), &Message{To: "not a number", Body: "hi"})
	if err == nil {
		t.Fatal("a message the provider refused was reported as sent")
	}
	if result.Success {
		t.Error("result says it was sent")
	}

	// And through the path a flow takes.
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "not a number",
		Payload: map[string]interface{}{"body": "hi"},
	}); err == nil {
		t.Error("a flow was told the message went out")
	}
}

func TestAWriteBecomesTheMessage(t *testing.T) {
	api := &twilioAPI{}
	c := api.serve(t, &Config{Driver: "twilio",
		Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s", From: "+6491234567"}})

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "+6497654321",
		Payload: map[string]interface{}{"body": "Your order shipped"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}
	// The destination comes from the flow's target when the payload has none.
	if api.form.Get("To") != "+6497654321" {
		t.Errorf("To = %q", api.form.Get("To"))
	}
}

func TestWhatCountsAsAMessage(t *testing.T) {
	// Payloads come from transforms, and each of these is a shape one produces.
	for name, tc := range map[string]struct {
		target  string
		payload interface{}
		want    Message
	}{
		"a record":                {"", map[string]interface{}{"to": "+64222", "body": "hi", "from": "+64111"}, Message{To: "+64222", Body: "hi", From: "+64111"}},
		"one that says text":      {"+64222", map[string]interface{}{"text": "hi"}, Message{To: "+64222", Body: "hi"}},
		"a line of text":          {"+64222", "hi", Message{To: "+64222", Body: "hi"}},
		"the message itself":      {"", &Message{To: "+64222", Body: "hi"}, Message{To: "+64222", Body: "hi"}},
		"a copy of the message":   {"", Message{To: "+64222", Body: "hi"}, Message{To: "+64222", Body: "hi"}},
		"a record with no number": {"+64999", map[string]interface{}{"body": "hi"}, Message{To: "+64999", Body: "hi"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := smsFromData(tc.target, tc.payload)
			if err != nil {
				t.Fatalf("smsFromData: %v", err)
			}
			if *got != tc.want {
				t.Errorf("message = %+v, want %+v", *got, tc.want)
			}
		})
	}

	if _, err := smsFromData("+64222", 42); err == nil {
		t.Error("a number was accepted as a text message")
	}
}

func TestTheProviderIsAskedWhetherItIsThere(t *testing.T) {
	api := &twilioAPI{answer: `{"sid":"AC-1","status":"active"}`, status: http.StatusOK}
	c := api.serve(t, &Config{Driver: "twilio",
		Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s"}})

	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if api.path != "/2010-04-01/Accounts/AC-1.json" {
		t.Errorf("checked %s", api.path)
	}

	// Credentials that no longer work are the commonest reason, and the check
	// exists to say so before a message is lost to it.
	api.status = http.StatusUnauthorized
	if err := c.Health(context.Background()); err == nil {
		t.Error("a provider that refused the credentials was reported healthy")
	}

	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
	if c.Name() != "sms" || c.Type() != "sms" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
}

func TestCredentialsAreCheckedBeforeSending(t *testing.T) {
	c := NewTwilioConnector("sms", &Config{Driver: "twilio", Twilio: &TwilioConfig{AccountSID: "AC-1"}})
	if err := c.Connect(context.Background()); err == nil {
		t.Error("a connector with no auth token said it was ready")
	}

	full := NewTwilioConnector("sms", &Config{Driver: "twilio",
		Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s"}})
	if err := full.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if full.config.Twilio.Timeout == 0 || full.config.Twilio.APIURL == "" {
		t.Errorf("defaults missing: %+v", full.config.Twilio)
	}
}

func TestAProviderThatCannotBeReached(t *testing.T) {
	c := NewTwilioConnector("sms", &Config{Driver: "twilio",
		Twilio: &TwilioConfig{AccountSID: "AC-1", AuthToken: "s", APIURL: "http://127.0.0.1:1", Timeout: time.Second}})

	if _, err := c.Send(context.Background(), &Message{To: "+64222", Body: "hi"}); err == nil {
		t.Error("a message was sent to a provider nobody is running")
	}
	if err := c.Health(context.Background()); err == nil {
		t.Error("a provider that is not there was reported healthy")
	}
}

// TestTheKindOfTrafficAMessageIsSentAs covers what SNS is actually asked for.
//
// Both settings were read from the configuration and then dropped. Promotional
// and Transactional are not interchangeable: promotional traffic is what
// carriers throttle first, and in several countries a one-time code sent as
// promotional is not delivered at all.
func TestTheKindOfTrafficAMessageIsSentAs(t *testing.T) {
	c := NewSNSConnector("sms", &Config{
		Driver: "sns",
		SNS:    &SNSConfig{Region: "ap-southeast-2", SenderID: "MYCEL", SMSType: "Transactional"},
	})

	input := c.publishInput(&Message{To: "+6497654321", Body: "Your code is 1234"})

	if aws.ToString(input.PhoneNumber) != "+6497654321" || aws.ToString(input.Message) != "Your code is 1234" {
		t.Fatalf("input = %+v", input)
	}
	kind, ok := input.MessageAttributes["AWS.SNS.SMS.SMSType"]
	if !ok || aws.ToString(kind.StringValue) != "Transactional" {
		t.Errorf("sms type = %v, want the configured one", input.MessageAttributes)
	}
	sender, ok := input.MessageAttributes["AWS.SNS.SMS.SenderID"]
	if !ok || aws.ToString(sender.StringValue) != "MYCEL" {
		t.Errorf("sender id = %v", input.MessageAttributes)
	}

	// Nothing configured means nothing sent, so the account's own settings
	// still apply rather than being overridden with blanks.
	plain := NewSNSConnector("sms", &Config{Driver: "sns", SNS: &SNSConfig{Region: "ap-southeast-2"}})
	if attrs := plain.publishInput(&Message{To: "+64222", Body: "hi"}).MessageAttributes; attrs != nil {
		t.Errorf("attributes = %v, want none", attrs)
	}
}

func TestSendingBeforeConnecting(t *testing.T) {
	// Connect is what builds the AWS client; without it there is nothing to
	// send through, and saying so beats a nil dereference.
	c := NewSNSConnector("sms", &Config{Driver: "sns", SNS: &SNSConfig{Region: "ap-southeast-2"}})

	result, err := c.Send(context.Background(), &Message{To: "+64222", Body: "hi"})
	if err == nil {
		t.Fatal("a message was sent through a connector that never connected")
	}
	if result.Success || result.Provider != "sns" {
		t.Errorf("result = %+v", result)
	}
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "+64222",
		Payload: map[string]interface{}{"body": "hi"},
	}); err == nil {
		t.Error("a flow was told the message went out")
	}
	if err := c.Health(context.Background()); err == nil {
		t.Error("a connector with no client reported itself healthy")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
	if c.Name() != "sms" || c.Type() != "sms" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
}

func TestHowSMSSettingsAreRead(t *testing.T) {
	props := map[string]interface{}{"timeout": "5s", "broken": "whenever"}
	if got := getDuration(props, "timeout", time.Second); got != 5*time.Second {
		t.Errorf("timeout = %v", got)
	}
	if got := getDuration(props, "broken", 30*time.Second); got != 30*time.Second {
		t.Errorf("unparseable timeout = %v, want the default", got)
	}
	if got := getDuration(props, "missing", 30*time.Second); got != 30*time.Second {
		t.Errorf("missing timeout = %v", got)
	}
}

func TestConnectingToSNS(t *testing.T) {
	// Credentials given in the configuration are used instead of whatever the
	// machine happens to have, which is what makes a deployment's identity
	// explicit rather than inherited from the host.
	c := NewSNSConnector("sms", &Config{
		Driver: "sns",
		SNS: &SNSConfig{
			Region:          "ap-southeast-2",
			AccessKeyID:     "AKIA-TEST",
			SecretAccessKey: "secret",
		},
	})

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if c.client == nil {
		t.Error("Connect returned without building a client")
	}
}

package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Sending a notification to an iPhone.
//
// Apple runs two services, and a token issued for one is meaningless to the
// other: send a real device's notification to the sandbox and it is accepted
// by nobody and reported by nothing. Which one a message goes to is decided
// here, from a single boolean, and it was never checked.

// recordedTransport answers every request without a server, and keeps the last
// one so the test can look at where it was going.
type recordedTransport struct {
	status  int
	headers http.Header

	request *http.Request
	body    []byte
}

func (rt *recordedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.request = r
	if r.Body != nil {
		rt.body, _ = io.ReadAll(r.Body)
	}
	status := rt.status
	if status == 0 {
		status = http.StatusOK
	}
	headers := rt.headers
	if headers == nil {
		headers = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}

func apnsWith(t *testing.T, cfg *APNsConfig) (*APNsConnector, *recordedTransport) {
	t.Helper()
	c := NewAPNsConnector("notifications", &Config{Driver: "apns", APNs: cfg})
	rt := &recordedTransport{}
	c.httpClient = &http.Client{Transport: rt}
	return c, rt
}

func TestWhichOfApplesTwoServicesIsUsed(t *testing.T) {
	for name, tc := range map[string]struct {
		config *APNsConfig
		want   string
	}{
		"a build under development goes to the sandbox": {
			&APNsConfig{BundleID: "nz.co.example.app"},
			"https://api.sandbox.push.apple.com/3/device/device-1",
		},
		"a released build goes to the live service": {
			&APNsConfig{BundleID: "nz.co.example.app", Production: true},
			"https://api.push.apple.com/3/device/device-1",
		},
		"and anything named explicitly wins over both": {
			&APNsConfig{BundleID: "nz.co.example.app", Production: true, APIURL: "https://apns.internal/"},
			"https://apns.internal/3/device/device-1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, rt := apnsWith(t, tc.config)

			if _, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Your order"}); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if got := rt.request.URL.String(); got != tc.want {
				t.Errorf("sent to %s, want %s", got, tc.want)
			}
		})
	}
}

func TestWhatAppleIsSent(t *testing.T) {
	c, rt := apnsWith(t, &APNsConfig{BundleID: "nz.co.example.app"})

	if _, err := c.Send(context.Background(), &Message{
		Token: "device-1",
		Title: "Your order",
		Body:  "On its way",
		Data:  map[string]string{"order_id": "order-1"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The bundle id says which app on the phone the notification belongs to,
	// and Apple refuses a message without it.
	if got := rt.request.Header.Get("apns-topic"); got != "nz.co.example.app" {
		t.Errorf("apns-topic = %q", got)
	}

	var sent map[string]interface{}
	if err := json.Unmarshal(rt.body, &sent); err != nil {
		t.Fatalf("body: %v", err)
	}
	aps, ok := sent["aps"].(map[string]interface{})
	if !ok {
		t.Fatalf("no aps section: %s", rt.body)
	}
	alert, ok := aps["alert"].(map[string]interface{})
	if !ok || alert["title"] != "Your order" || alert["body"] != "On its way" {
		t.Errorf("alert = %v", aps["alert"])
	}
	// Custom data rides alongside aps, not inside it — inside, Apple drops it.
	if sent["order_id"] != "order-1" {
		t.Errorf("custom data = %v, want it beside aps", sent)
	}
}

func TestApplesIdentifierIsKept(t *testing.T) {
	// It is the only handle on a notification afterwards: without it, a
	// complaint that a notification never arrived cannot be looked up.
	c, rt := apnsWith(t, &APNsConfig{BundleID: "nz.co.example.app"})
	rt.headers = http.Header{"Apns-Id": []string{"apns-message-1"}}

	result, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.MessageID != "apns-message-1" {
		t.Errorf("message id = %q", result.MessageID)
	}
	if result.Provider != "apns" {
		t.Errorf("provider = %q", result.Provider)
	}
}

func TestASendAppleRefusedIsAFailure(t *testing.T) {
	// The commonest one is BadDeviceToken, and it is the answer that tells a
	// service to stop sending to that device.
	c, rt := apnsWith(t, &APNsConfig{BundleID: "nz.co.example.app"})
	rt.status = http.StatusBadRequest

	result, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"})
	if err == nil {
		t.Fatal("a notification Apple refused was reported as delivered")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v", result)
	}
	if !strings.Contains(result.Error, "400") {
		t.Errorf("error = %q, want it to say what Apple answered", result.Error)
	}
}

func TestANotificationAddressedToNobody(t *testing.T) {
	c, _ := apnsWith(t, &APNsConfig{BundleID: "nz.co.example.app"})

	result, err := c.Send(context.Background(), &Message{Title: "Hi"})
	if err == nil {
		t.Fatal("a notification with no device to send it to was accepted")
	}
	if result.Success {
		t.Error("result says it was sent")
	}
}

func TestOnlyOnePhoneAtATime(t *testing.T) {
	// Apple's service takes one device per request, so a message carrying
	// several tokens goes to the first — worth knowing rather than assuming
	// the rest were sent too.
	c, rt := apnsWith(t, &APNsConfig{BundleID: "nz.co.example.app"})

	if _, err := c.Send(context.Background(), &Message{
		Tokens: []string{"device-1", "device-2"},
		Title:  "Hi",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.HasSuffix(rt.request.URL.String(), "/3/device/device-1") {
		t.Errorf("sent to %s", rt.request.URL)
	}
}

func TestAnAppleWriteBecomesTheNotification(t *testing.T) {
	c, rt := apnsWith(t, &APNsConfig{BundleID: "nz.co.example.app"})

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "device-1",
		Payload: map[string]interface{}{"title": "Your order"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result == nil || result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}
	if !strings.HasSuffix(rt.request.URL.String(), "/3/device/device-1") {
		t.Errorf("sent to %s, want the flow's target", rt.request.URL)
	}

	rt.status = http.StatusGone // the device token is no longer valid
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "device-1",
		Payload: map[string]interface{}{"title": "Your order"},
	}); err == nil {
		t.Error("a notification nobody received was reported as sent")
	}
}

func TestApplesCredentialsAreRequiredBeforeSending(t *testing.T) {
	// Connect is where a missing key is reported, and it is the difference
	// between a service that refuses to start and one that fails silently on
	// the first notification.
	incomplete := NewAPNsConnector("notifications", &Config{
		Driver: "apns",
		APNs:   &APNsConfig{TeamID: "TEAM", BundleID: "nz.co.example.app"},
	})
	if err := incomplete.Connect(context.Background()); err == nil {
		t.Error("a connector with no signing key said it was ready")
	}

	complete := NewAPNsConnector("notifications", &Config{
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM",
			KeyID:      "KEY",
			PrivateKey: "-----BEGIN PRIVATE KEY-----",
			BundleID:   "nz.co.example.app",
		},
	})
	if err := complete.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if complete.config.APNs.Timeout == 0 {
		t.Error("no timeout, so a service that never answers holds the flow for ever")
	}
	if err := complete.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if err := complete.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
	if complete.Name() != "notifications" || complete.Type() != "push" {
		t.Errorf("name/type = %s/%s", complete.Name(), complete.Type())
	}
}

func TestHowSettingsAreRead(t *testing.T) {
	// These come out of HCL, where a number may arrive as a number, a string,
	// or a float — and a time to live silently read as zero is a notification
	// that expires immediately.
	for name, tc := range map[string]struct {
		value interface{}
		want  int
		ok    bool
	}{
		"a whole number":      {3600, 3600, true},
		"a wide one":          {int64(3600), 3600, true},
		"one JSON decoded":    {float64(3600), 3600, true},
		"one written as text": {"3600", 3600, true},
		"not a number":        {"soon", 0, false},
		"nothing at all":      {nil, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := intOf(tc.value)
			if got != tc.want || ok != tc.ok {
				t.Errorf("intOf(%v) = %d, %v; want %d, %v", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}

	props := map[string]interface{}{"timeout": "5s", "broken": "whenever", "production": true}
	if got := getDuration(props, "timeout", time.Second); got != 5*time.Second {
		t.Errorf("timeout = %v", got)
	}
	// Something unparseable falls back rather than becoming zero, which would
	// be a client with no timeout at all.
	if got := getDuration(props, "broken", 30*time.Second); got != 30*time.Second {
		t.Errorf("unparseable timeout = %v, want the default", got)
	}
	if got := getDuration(props, "missing", 30*time.Second); got != 30*time.Second {
		t.Errorf("missing timeout = %v", got)
	}
	if !getBool(props, "production", false) {
		t.Error("production was set and read as false")
	}
	if getBool(props, "missing", false) {
		t.Error("a setting nobody wrote came back true")
	}
}

package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Sending a notification to a phone.
//
// What a device receives is decided here: which devices a message is addressed
// to, whether it arrives as a notification the system shows or as data the app
// handles, and — the one that matters over time — which tokens the service told
// us are dead. A token nobody removes is a notification sent for ever to an app
// that was uninstalled.
//
// All of it goes through the v1 API. The connector used to post to /fcm/send
// with a server key, which Google retired in June 2024, so every one of these
// exercised a request Firebase no longer answers.

// firebase stands in for Google: the token endpoint and the send endpoint.
type firebase struct {
	status int
	answer string
	calls  int32

	lastBody map[string]interface{}
	lastAuth string
	lastPath string
	tokens   int32
}

// serviceAccountJSON is a real key in the shape Firebase hands out, generated
// here so that the signing is exercised rather than stubbed.
func serviceAccountJSON(t *testing.T, tokenURL string) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	account, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "the-project",
		"private_key":  string(pemKey),
		"client_email": "push@the-project.iam.gserviceaccount.com",
		"token_uri":    tokenURL,
	})
	if err != nil {
		t.Fatalf("marshal account: %v", err)
	}
	return string(account)
}

func (f *firebase) serve(t *testing.T) *FCMConnector {
	t.Helper()
	if f.status == 0 {
		f.status = http.StatusOK
	}
	if f.answer == "" {
		f.answer = `{"name":"projects/the-project/messages/m-1"}`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			atomic.AddInt32(&f.tokens, 1)
			_, _ = w.Write([]byte(`{"access_token":"an-access-token","expires_in":3600}`))
			return
		}
		atomic.AddInt32(&f.calls, 1)
		f.lastAuth = r.Header.Get("Authorization")
		f.lastPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&f.lastBody)
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.answer))
	}))
	t.Cleanup(server.Close)

	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM: &FCMConfig{
			ServiceAccountJSON: serviceAccountJSON(t, server.URL+"/token"),
			APIURL:             server.URL,
		},
	})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return c
}

// sentMessage digs out the message the v1 API wraps everything in.
func (f *firebase) sentMessage(t *testing.T) map[string]interface{} {
	t.Helper()
	message, ok := f.lastBody["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("the request has no message in it: %v", f.lastBody)
	}
	return message
}

func TestWhoANotificationIsAddressedTo(t *testing.T) {
	// Four ways, and sending to the wrong one is either nobody or everybody.
	for name, tc := range map[string]struct {
		message *Message
		check   func(t *testing.T, sent map[string]interface{})
	}{
		"one device": {
			&Message{Token: "device-1", Title: "Your order", Body: "On its way"},
			func(t *testing.T, sent map[string]interface{}) {
				if sent["token"] != "device-1" {
					t.Errorf("token = %v", sent["token"])
				}
			},
		},
		"everybody subscribed to a topic": {
			&Message{Topic: "orders", Title: "Your order"},
			func(t *testing.T, sent map[string]interface{}) {
				// A field of its own in this API, where the legacy one wanted
				// the topic smuggled into the address with a /topics/ prefix.
				if sent["topic"] != "orders" {
					t.Errorf("topic = %v", sent["topic"])
				}
				if sent["token"] != nil {
					t.Errorf("a topic message also carried a token: %v", sent["token"])
				}
			},
		},
		"whoever matches a condition": {
			&Message{Condition: "'orders' in topics", Title: "Your order"},
			func(t *testing.T, sent map[string]interface{}) {
				if sent["condition"] == nil {
					t.Errorf("sent = %v, want the condition", sent)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			api := &firebase{}
			c := api.serve(t)

			if _, err := c.Send(context.Background(), tc.message); err != nil {
				t.Fatalf("Send: %v", err)
			}
			tc.check(t, api.sentMessage(t))
		})
	}
}

func TestSeveralDevicesAreSentOneMessageEach(t *testing.T) {
	// The v1 API takes one target per request where the legacy one took a
	// list, so a device that has uninstalled the app fails on its own rather
	// than failing the batch.
	api := &firebase{}
	c := api.serve(t)

	if _, err := c.Send(context.Background(), &Message{
		Tokens: []string{"device-1", "device-2", "device-3"},
		Title:  "Your order",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := atomic.LoadInt32(&api.calls); got != 3 {
		t.Errorf("%d requests for three devices", got)
	}
}

func TestWhatTheDeviceIsSent(t *testing.T) {
	api := &firebase{}
	c := api.serve(t)

	if _, err := c.Send(context.Background(), &Message{
		Token:       "device-1",
		Title:       "Your order",
		Body:        "On its way",
		Data:        map[string]string{"order_id": "order-1"},
		Priority:    "high",
		TTL:         3600,
		CollapseKey: "orders",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// A bearer token from the service account, not a shared key. Without it
	// every send is refused with something that names neither the service nor
	// the message.
	if api.lastAuth != "Bearer an-access-token" {
		t.Errorf("authorization = %q", api.lastAuth)
	}
	// And the project is in the path, which is how this API knows whose
	// messages these are.
	if !strings.Contains(api.lastPath, "/v1/projects/the-project/messages:send") {
		t.Errorf("path = %q", api.lastPath)
	}

	sent := api.sentMessage(t)
	notification, ok := sent["notification"].(map[string]interface{})
	if !ok || notification["title"] != "Your order" {
		t.Errorf("notification = %v", sent["notification"])
	}
	// Data is what the app reads when it handles the message itself.
	data, ok := sent["data"].(map[string]interface{})
	if !ok || data["order_id"] != "order-1" {
		t.Errorf("data = %v", sent["data"])
	}

	// Priority, time to live and the collapse key moved under android in this
	// API, and a message that kept them at the top level is refused.
	android, ok := sent["android"].(map[string]interface{})
	if !ok {
		t.Fatalf("no android block: %v", sent)
	}
	if android["priority"] != "high" {
		t.Errorf("priority = %v, which is what wakes a sleeping phone", android["priority"])
	}
	// A duration with a unit, not a number of seconds.
	if android["ttl"] != "3600s" {
		t.Errorf("ttl = %v, want a duration string", android["ttl"])
	}
	// The collapse key is what stops five notifications about the same order.
	if android["collapse_key"] != "orders" {
		t.Errorf("collapse key = %v", android["collapse_key"])
	}
}

func TestDataValuesBecomeStrings(t *testing.T) {
	// This API refuses anything else, and a flow putting a number in there
	// would otherwise get a 400 naming a field it did not write.
	api := &firebase{}
	c := api.serve(t)

	if _, err := c.Send(context.Background(), &Message{
		Token: "device-1",
		Data:  map[string]string{"count": "3"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	data, ok := api.sentMessage(t)["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v", api.sentMessage(t)["data"])
	}
	if _, isString := data["count"].(string); !isString {
		t.Errorf("count = %#v, want a string", data["count"])
	}
}

func TestTheDevicesThatCouldNotBeReachedAreNamed(t *testing.T) {
	// A token dies when an app is uninstalled, and a service that never learns
	// which ones sends to them for ever.
	var refuseSecond int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			_, _ = w.Write([]byte(`{"access_token":"an-access-token","expires_in":3600}`))
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		message, _ := body["message"].(map[string]interface{})
		if message["token"] == "device-2" {
			atomic.AddInt32(&refuseSecond, 1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","message":"Requested entity was not found."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"projects/the-project/messages/m-1"}`))
	}))
	defer server.Close()

	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM: &FCMConfig{
			ServiceAccountJSON: serviceAccountJSON(t, server.URL+"/token"),
			APIURL:             server.URL,
		},
	})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	result, err := c.Send(context.Background(), &Message{
		Tokens: []string{"device-1", "device-2"},
		Title:  "Your order",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(result.FailedTokens) != 1 || result.FailedTokens[0] != "device-2" {
		t.Errorf("failed tokens = %v, want the one the service refused", result.FailedTokens)
	}
	// The send is still a success: one device got it.
	if !result.Success {
		t.Error("a send that reached one of two devices was reported as a failure")
	}
}

func TestASendTheServiceRefusedIsAFailure(t *testing.T) {
	// A revoked service account or a quota: a flow reading these as sent will
	// never send the notification again.
	api := &firebase{status: http.StatusForbidden, answer: `{"error":{"status":"PERMISSION_DENIED"}}`}
	c := api.serve(t)

	result, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"})
	if err == nil {
		t.Fatal("a send the service refused was reported as delivered")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v", result)
	}
}

func TestTheAccessTokenIsNotFetchedForEveryMessage(t *testing.T) {
	// It is good for an hour, and asking Google for one per notification is a
	// request per notification that nobody needs to make.
	api := &firebase{}
	c := api.serve(t)

	for i := 0; i < 4; i++ {
		if _, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if got := atomic.LoadInt32(&api.tokens); got != 1 {
		t.Errorf("asked for %d access tokens to send four notifications", got)
	}
}

func TestAWriteBecomesTheNotification(t *testing.T) {
	// The path a flow takes: a `to` block with a push connector in it.
	api := &firebase{}
	c := api.serve(t)

	result, err := c.Write(context.Background(), &connector.Data{
		Target: "device-1",
		Payload: map[string]interface{}{
			"title": "Your order",
			"body":  "On its way",
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result == nil || len(result.Rows) == 0 {
		t.Errorf("result = %+v", result)
	}
	if api.sentMessage(t)["token"] != "device-1" {
		t.Errorf("token = %v, want the flow's target", api.sentMessage(t)["token"])
	}
}

func TestAWriteThatCouldNotBeSentFailsTheFlow(t *testing.T) {
	api := &firebase{status: http.StatusInternalServerError, answer: `{"error":{"status":"UNAVAILABLE"}}`}
	c := api.serve(t)

	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "device-1",
		Payload: map[string]interface{}{"title": "Your order"},
	}); err == nil {
		t.Error("a notification nobody received was reported as sent")
	}
}

func TestAServiceThatCannotBeReachedIsReported(t *testing.T) {
	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM: &FCMConfig{
			ServiceAccountJSON: serviceAccountJSON(t, "http://127.0.0.1:1/token"),
			APIURL:             "http://127.0.0.1:1",
		},
	})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if _, err := c.Send(context.Background(), &Message{Token: "device-1", Title: "Hi"}); err == nil {
		t.Error("a notification was sent to a service nobody is running")
	}
}

func TestTheLegacyServerKeyIsRefusedRatherThanTried(t *testing.T) {
	// Google retired that API in June 2024. A service configured with a server
	// key would otherwise start, look healthy, and fail every push against an
	// endpoint that no longer exists.
	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM:    &FCMConfig{ServerKey: "the-old-server-key"},
	})

	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("a connector configured with only the legacy key started")
	}
	// And it says what to write instead, or somebody has to go and find out.
	if !strings.Contains(err.Error(), "service_account_json") {
		t.Errorf("the error does not say what to use: %v", err)
	}
}

func TestAServiceAccountThatCannotSignIsRefusedAtStartup(t *testing.T) {
	// A key that cannot be parsed should stop a deployment, not a
	// notification three days later.
	for name, account := range map[string]string{
		"not JSON at all":  "this is not a service account",
		"JSON with no key": `{"type":"service_account","project_id":"p","client_email":"a@b.c"}`,
		"a key that is not a key": `{"type":"service_account","project_id":"p","client_email":"a@b.c",` +
			`"private_key":"-----BEGIN PRIVATE KEY-----\nbm90IGEga2V5\n-----END PRIVATE KEY-----\n"}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := NewFCMConnector("notifications", &Config{
				Driver: "fcm",
				FCM:    &FCMConfig{ServiceAccountJSON: account},
			})
			if err := c.Connect(context.Background()); err == nil {
				t.Error("a service account nothing can sign with was accepted")
			}
		})
	}
}

func TestTheProjectComesFromTheServiceAccountWhenNobodySaid(t *testing.T) {
	// It is in the file already, and asking somebody to write it twice is a
	// way to have it written differently.
	api := &firebase{}
	c := api.serve(t)

	if c.projectID() != "the-project" {
		t.Errorf("project = %q, want the one in the service account", c.projectID())
	}
}

func TestTheAddressIsTidiedUp(t *testing.T) {
	// A trailing slash in the configuration would produce a double slash in
	// the path, which some servers answer and others do not.
	c := NewFCMConnector("notifications", &Config{
		Driver: "fcm",
		FCM:    &FCMConfig{ServiceAccountJSON: "{}", APIURL: "https://fcm.example.com/"},
	})
	if c.config.FCM.APIURL != "https://fcm.example.com" {
		t.Errorf("api url = %q", c.config.FCM.APIURL)
	}

	// And nothing said is the real service.
	plain := NewFCMConnector("notifications", &Config{Driver: "fcm", FCM: &FCMConfig{}})
	if plain.config.FCM.APIURL == "" {
		t.Error("no address at all, so every send goes nowhere")
	}
	if plain.config.FCM.Timeout == 0 {
		t.Error("no timeout, so a service that never answers holds the flow for ever")
	}
}

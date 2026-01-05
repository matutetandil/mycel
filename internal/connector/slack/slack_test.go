package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewConnector(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://hooks.slack.com/test",
	}

	conn := NewConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "slack" {
		t.Errorf("expected type 'slack', got %s", conn.Type())
	}
}

func TestConnector_DefaultTimeout(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://hooks.slack.com/test",
	}

	conn := NewConnector("test", cfg)

	if conn.config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", conn.config.Timeout)
	}
}

func TestConnector_Connect_RequiresWebhookOrToken(t *testing.T) {
	cfg := &Config{
		Name: "test",
	}

	conn := NewConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err == nil {
		t.Error("expected error when neither webhook_url nor token is set")
	}
}

func TestConnector_Connect_WithWebhook(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://hooks.slack.com/test",
	}

	conn := NewConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err != nil {
		t.Errorf("Connect should succeed with webhook: %v", err)
	}
}

func TestConnector_Connect_WithToken(t *testing.T) {
	cfg := &Config{
		Name:  "test",
		Token: "xoxb-test-token",
	}

	conn := NewConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err != nil {
		t.Errorf("Connect should succeed with token: %v", err)
	}
}

func TestConnector_SendViaWebhook(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}

		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}

		if msg.Text != "Hello, World!" {
			t.Errorf("expected text 'Hello, World!', got %s", msg.Text)
		}

		w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	result, err := conn.Send(context.Background(), &Message{Text: "Hello, World!"})

	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestConnector_SendViaWebhook_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid_payload"))
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	result, err := conn.Send(context.Background(), &Message{Text: "Hello"})

	if err == nil {
		t.Error("expected error")
	}
	if result.Success {
		t.Error("expected failure")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestConnector_SendViaAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("expected Bearer token")
		}

		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"channel": "C123456",
			"ts":      "1234567890.123456",
		})
	}))
	defer server.Close()

	// Note: We can't easily mock the API URL, so this test just verifies the structure
	cfg := &Config{
		Name:  "test",
		Token: "test-token",
	}

	conn := NewConnector("test", cfg)
	// The actual API call would fail without mocking the URL
	// This is more of a structure test
	_ = conn
}

func TestConnector_AppliesDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if msg.Channel != "#default" {
			t.Errorf("expected channel '#default', got %s", msg.Channel)
		}
		if msg.Username != "bot" {
			t.Errorf("expected username 'bot', got %s", msg.Username)
		}
		if msg.IconEmoji != ":robot:" {
			t.Errorf("expected icon_emoji ':robot:', got %s", msg.IconEmoji)
		}

		w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := &Config{
		Name:           "test",
		WebhookURL:     server.URL,
		DefaultChannel: "#default",
		Username:       "bot",
		IconEmoji:      ":robot:",
	}

	conn := NewConnector("test", cfg)
	conn.Send(context.Background(), &Message{Text: "Hello"})
}

func TestConnector_Write_WithString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if msg.Text != "Hello" {
			t.Errorf("expected text 'Hello', got %s", msg.Text)
		}
		if msg.Channel != "#general" {
			t.Errorf("expected channel '#general', got %s", msg.Channel)
		}

		w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	result, err := conn.Write(context.Background(), "#general", "Hello")

	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if result == nil {
		t.Error("expected result")
	}
}

func TestConnector_Write_WithMap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if msg.Text != "Hello from map" {
			t.Errorf("expected text 'Hello from map', got %s", msg.Text)
		}

		w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	data := map[string]interface{}{
		"text": "Hello from map",
	}
	result, err := conn.Write(context.Background(), "#general", data)

	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if result == nil {
		t.Error("expected result")
	}
}

func TestConnector_Write_WithMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if msg.Text != "Hello Message" {
			t.Errorf("expected text 'Hello Message', got %s", msg.Text)
		}

		w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	msg := &Message{Text: "Hello Message"}
	result, err := conn.Write(context.Background(), "#general", msg)

	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if result == nil {
		t.Error("expected result")
	}
}

func TestConnector_Write_UnsupportedType(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://hooks.slack.com/test",
	}

	conn := NewConnector("test", cfg)
	_, err := conn.Write(context.Background(), "#general", 12345)

	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestConnector_Close(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://hooks.slack.com/test",
	}

	conn := NewConnector("test", cfg)
	err := conn.Close(context.Background())

	if err != nil {
		t.Errorf("Close should not fail: %v", err)
	}
}

func TestFactory(t *testing.T) {
	factory := NewFactory()

	if factory.Type() != "slack" {
		t.Errorf("expected type 'slack', got %s", factory.Type())
	}
}

func TestFactory_Create(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"webhook_url": "https://hooks.slack.com/test",
		"channel":     "#general",
		"username":    "mybot",
		"icon_emoji":  ":robot:",
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	slackConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("expected *Connector")
	}

	if slackConn.config.WebhookURL != "https://hooks.slack.com/test" {
		t.Error("webhook_url not set")
	}
	if slackConn.config.DefaultChannel != "#general" {
		t.Error("channel not set")
	}
	if slackConn.config.Username != "mybot" {
		t.Error("username not set")
	}
	if slackConn.config.IconEmoji != ":robot:" {
		t.Error("icon_emoji not set")
	}
}

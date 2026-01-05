package discord

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
		WebhookURL: "https://discord.com/api/webhooks/test",
	}

	conn := NewConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "discord" {
		t.Errorf("expected type 'discord', got %s", conn.Type())
	}
}

func TestConnector_DefaultTimeout(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://discord.com/api/webhooks/test",
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
		t.Error("expected error when neither webhook_url nor bot_token is set")
	}
}

func TestConnector_Connect_WithWebhook(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://discord.com/api/webhooks/test",
	}

	conn := NewConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err != nil {
		t.Errorf("Connect should succeed with webhook: %v", err)
	}
}

func TestConnector_Connect_WithToken(t *testing.T) {
	cfg := &Config{
		Name:     "test",
		BotToken: "test-token",
	}

	conn := NewConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err != nil {
		t.Errorf("Connect should succeed with token: %v", err)
	}
}

func TestConnector_SendViaWebhook(t *testing.T) {
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

		if msg.Content != "Hello, World!" {
			t.Errorf("expected content 'Hello, World!', got %s", msg.Content)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "123456789",
			"channel_id": "987654321",
		})
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	result, err := conn.Send(context.Background(), &Message{Content: "Hello, World!"})

	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.MessageID != "123456789" {
		t.Errorf("expected message ID '123456789', got %s", result.MessageID)
	}
}

func TestConnector_SendViaWebhook_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "invalid payload"}`))
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	result, err := conn.Send(context.Background(), &Message{Content: "Hello"})

	if err == nil {
		t.Error("expected error")
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestConnector_AppliesDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if msg.Username != "mybot" {
			t.Errorf("expected username 'mybot', got %s", msg.Username)
		}
		if msg.AvatarURL != "https://example.com/avatar.png" {
			t.Errorf("expected avatar_url, got %s", msg.AvatarURL)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
		Username:   "mybot",
		AvatarURL:  "https://example.com/avatar.png",
	}

	conn := NewConnector("test", cfg)
	conn.Send(context.Background(), &Message{Content: "Hello"})
}

func TestConnector_Write_WithString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if msg.Content != "Hello" {
			t.Errorf("expected content 'Hello', got %s", msg.Content)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	result, err := conn.Write(context.Background(), "", "Hello")

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

		if msg.Content != "Hello from map" {
			t.Errorf("expected content 'Hello from map', got %s", msg.Content)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	data := map[string]interface{}{
		"content": "Hello from map",
	}
	result, err := conn.Write(context.Background(), "", data)

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

		if msg.Content != "Hello Message" {
			t.Errorf("expected content 'Hello Message', got %s", msg.Content)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	msg := &Message{Content: "Hello Message"}
	result, err := conn.Write(context.Background(), "", msg)

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
		WebhookURL: "https://discord.com/api/webhooks/test",
	}

	conn := NewConnector("test", cfg)
	_, err := conn.Write(context.Background(), "", 12345)

	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestConnector_SendViaAPI_RequiresChannelID(t *testing.T) {
	cfg := &Config{
		Name:     "test",
		BotToken: "test-token",
	}

	conn := NewConnector("test", cfg)
	result, err := conn.sendViaAPI(context.Background(), &Message{Content: "Hello"}, "")

	if err == nil {
		t.Error("expected error when channel_id is empty")
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestConnector_Close(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		WebhookURL: "https://discord.com/api/webhooks/test",
	}

	conn := NewConnector("test", cfg)
	err := conn.Close(context.Background())

	if err != nil {
		t.Errorf("Close should not fail: %v", err)
	}
}

func TestFactory(t *testing.T) {
	factory := NewFactory()

	if factory.Type() != "discord" {
		t.Errorf("expected type 'discord', got %s", factory.Type())
	}
}

func TestFactory_Create(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"webhook_url": "https://discord.com/api/webhooks/test",
		"username":    "mybot",
		"avatar_url":  "https://example.com/avatar.png",
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	discordConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("expected *Connector")
	}

	if discordConn.config.WebhookURL != "https://discord.com/api/webhooks/test" {
		t.Error("webhook_url not set")
	}
	if discordConn.config.Username != "mybot" {
		t.Error("username not set")
	}
	if discordConn.config.AvatarURL != "https://example.com/avatar.png" {
		t.Error("avatar_url not set")
	}
}

func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)

		if len(msg.Embeds) != 1 {
			t.Errorf("expected 1 embed, got %d", len(msg.Embeds))
		}
		if msg.Embeds[0].Title != "Test Embed" {
			t.Errorf("expected title 'Test Embed', got %s", msg.Embeds[0].Title)
		}
		if msg.Embeds[0].Color != 0xFF0000 {
			t.Errorf("expected color 0xFF0000, got %d", msg.Embeds[0].Color)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "123"})
	}))
	defer server.Close()

	cfg := &Config{
		Name:       "test",
		WebhookURL: server.URL,
	}

	conn := NewConnector("test", cfg)
	msg := &Message{
		Embeds: []Embed{
			{
				Title:       "Test Embed",
				Description: "This is a test",
				Color:       0xFF0000,
				Fields: []EmbedField{
					{Name: "Field 1", Value: "Value 1", Inline: true},
				},
			},
		},
	}
	result, err := conn.Send(context.Background(), msg)

	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

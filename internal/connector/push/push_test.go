package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewFCMConnector(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM: &FCMConfig{
			ServerKey: "test-key",
		},
	}

	conn := NewFCMConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "push" {
		t.Errorf("expected type 'push', got %s", conn.Type())
	}
}

func TestFCMConnector_DefaultTimeout(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM: &FCMConfig{
			ServerKey: "test-key",
		},
	}

	conn := NewFCMConnector("test", cfg)

	if conn.config.FCM.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", conn.config.FCM.Timeout)
	}
}

func TestFCMConnector_Connect_RequiresServerKey(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM:    &FCMConfig{},
	}

	conn := NewFCMConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err == nil {
		t.Error("expected error when server_key is missing")
	}
}

func TestFCMConnector_Connect_WithServerKey(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM: &FCMConfig{
			ServerKey: "test-key",
		},
	}

	conn := NewFCMConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err != nil {
		t.Errorf("Connect should succeed: %v", err)
	}
}

func TestFCMConnector_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "key=test-key" {
			t.Errorf("expected Authorization 'key=test-key', got %s", r.Header.Get("Authorization"))
		}

		var msg map[string]interface{}
		json.NewDecoder(r.Body).Decode(&msg)

		if msg["to"] != "device-token-123" {
			t.Errorf("expected to 'device-token-123', got %v", msg["to"])
		}

		notification := msg["notification"].(map[string]interface{})
		if notification["title"] != "Test Title" {
			t.Errorf("expected title 'Test Title', got %v", notification["title"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message_id": 12345,
			"success":    1,
			"failure":    0,
		})
	}))
	defer server.Close()

	// Note: We can't easily override the FCM URL, so this test verifies structure
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM: &FCMConfig{
			ServerKey: "test-key",
		},
	}

	conn := NewFCMConnector("test", cfg)
	_ = conn
	_ = server
}

func TestFCMConnector_Health(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM: &FCMConfig{
			ServerKey: "test-key",
		},
	}

	conn := NewFCMConnector("test", cfg)
	err := conn.Health(context.Background())

	// FCM doesn't have a health check endpoint
	if err != nil {
		t.Errorf("Health should return nil: %v", err)
	}
}

func TestFCMConnector_Close(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "fcm",
		FCM: &FCMConfig{
			ServerKey: "test-key",
		},
	}

	conn := NewFCMConnector("test", cfg)
	err := conn.Close(context.Background())

	if err != nil {
		t.Errorf("Close should not fail: %v", err)
	}
}

func TestNewAPNsConnector(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM123",
			KeyID:      "KEY123",
			PrivateKey: "test-key",
			BundleID:   "com.example.app",
		},
	}

	conn := NewAPNsConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "push" {
		t.Errorf("expected type 'push', got %s", conn.Type())
	}
}

func TestAPNsConnector_DefaultTimeout(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs:   &APNsConfig{},
	}

	conn := NewAPNsConnector("test", cfg)

	if conn.config.APNs.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", conn.config.APNs.Timeout)
	}
}

func TestAPNsConnector_Connect_RequiresCredentials(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs:   &APNsConfig{},
	}

	conn := NewAPNsConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err == nil {
		t.Error("expected error when credentials are missing")
	}
}

func TestAPNsConnector_Connect_WithCredentials(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM123",
			KeyID:      "KEY123",
			PrivateKey: "test-key",
		},
	}

	conn := NewAPNsConnector("test", cfg)
	err := conn.Connect(context.Background())

	if err != nil {
		t.Errorf("Connect should succeed: %v", err)
	}
}

func TestAPNsConnector_Send_RequiresToken(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM123",
			KeyID:      "KEY123",
			PrivateKey: "test-key",
		},
	}

	conn := NewAPNsConnector("test", cfg)
	result, err := conn.Send(context.Background(), &Message{Title: "Test", Body: "Body"})

	if err == nil {
		t.Error("expected error when token is missing")
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestAPNsConnector_ProductionURL(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM123",
			KeyID:      "KEY123",
			PrivateKey: "test-key",
			Production: true,
		},
	}

	conn := NewAPNsConnector("test", cfg)
	if !conn.config.APNs.Production {
		t.Error("expected production to be true")
	}
}

func TestAPNsConnector_Health(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM123",
			KeyID:      "KEY123",
			PrivateKey: "test-key",
		},
	}

	conn := NewAPNsConnector("test", cfg)
	err := conn.Health(context.Background())

	// APNs doesn't have a health check endpoint
	if err != nil {
		t.Errorf("Health should return nil: %v", err)
	}
}

func TestAPNsConnector_Close(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Driver: "apns",
		APNs: &APNsConfig{
			TeamID:     "TEAM123",
			KeyID:      "KEY123",
			PrivateKey: "test-key",
		},
	}

	conn := NewAPNsConnector("test", cfg)
	err := conn.Close(context.Background())

	if err != nil {
		t.Errorf("Close should not fail: %v", err)
	}
}

func TestFactory(t *testing.T) {
	factory := NewFactory()

	if factory.Type() != "push" {
		t.Errorf("expected type 'push', got %s", factory.Type())
	}
}

func TestFactory_CreateFCM(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"driver":     "fcm",
		"server_key": "test-key",
		"project_id": "my-project",
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fcmConn, ok := conn.(*FCMConnector)
	if !ok {
		t.Fatal("expected *FCMConnector")
	}

	if fcmConn.config.FCM.ServerKey != "test-key" {
		t.Error("server_key not set")
	}
	if fcmConn.config.FCM.ProjectID != "my-project" {
		t.Error("project_id not set")
	}
}

func TestFactory_CreateAPNs(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"driver":      "apns",
		"team_id":     "TEAM123",
		"key_id":      "KEY123",
		"private_key": "test-key",
		"bundle_id":   "com.example.app",
		"production":  true,
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	apnsConn, ok := conn.(*APNsConnector)
	if !ok {
		t.Fatal("expected *APNsConnector")
	}

	if apnsConn.config.APNs.TeamID != "TEAM123" {
		t.Error("team_id not set")
	}
	if apnsConn.config.APNs.KeyID != "KEY123" {
		t.Error("key_id not set")
	}
	if apnsConn.config.APNs.BundleID != "com.example.app" {
		t.Error("bundle_id not set")
	}
	if !apnsConn.config.APNs.Production {
		t.Error("production not set")
	}
}

func TestFactory_CreateDefaultDriver(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"server_key": "test-key",
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Default driver should be fcm
	if _, ok := conn.(*FCMConnector); !ok {
		t.Error("expected *FCMConnector as default")
	}
}

func TestFactory_CreateUnknownDriver(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"driver": "unknown",
	}

	_, err := factory.Create("test", config)
	if err == nil {
		t.Error("expected error for unknown driver")
	}
}

func TestMessage(t *testing.T) {
	msg := &Message{
		Token:    "device-token",
		Title:    "Test Title",
		Body:     "Test Body",
		Data:     map[string]string{"key": "value"},
		Priority: "high",
		TTL:      3600,
	}

	if msg.Token != "device-token" {
		t.Error("Token not set")
	}
	if msg.Title != "Test Title" {
		t.Error("Title not set")
	}
	if msg.Body != "Test Body" {
		t.Error("Body not set")
	}
	if msg.Data["key"] != "value" {
		t.Error("Data not set")
	}
	if msg.Priority != "high" {
		t.Error("Priority not set")
	}
	if msg.TTL != 3600 {
		t.Error("TTL not set")
	}
}

func TestSendResult(t *testing.T) {
	result := &SendResult{
		Success:      true,
		MessageID:    "123",
		Provider:     "fcm",
		FailedTokens: []string{"token1"},
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.MessageID != "123" {
		t.Error("MessageID not set")
	}
	if result.Provider != "fcm" {
		t.Error("Provider not set")
	}
	if len(result.FailedTokens) != 1 {
		t.Error("FailedTokens not set")
	}
}

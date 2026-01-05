package email

import (
	"context"
	"testing"
	"time"
)

func TestNewSMTPConnector(t *testing.T) {
	cfg := &Config{
		Name: "test",
		SMTP: &SMTPConfig{
			Host:     "localhost",
			Port:     587,
			Username: "user",
			Password: "pass",
		},
	}

	conn := NewSMTPConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "email" {
		t.Errorf("expected type 'email', got %s", conn.Type())
	}
}

func TestSMTPConnectorDefaults(t *testing.T) {
	cfg := &Config{
		Name: "test",
	}

	conn := NewSMTPConnector("test", cfg)

	if conn.config.SMTP == nil {
		t.Error("SMTP config should have defaults")
	}
	if conn.config.SMTP.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", conn.config.SMTP.Timeout)
	}
	if conn.config.SMTP.PoolSize != 5 {
		t.Errorf("expected pool size 5, got %d", conn.config.SMTP.PoolSize)
	}
}

func TestBuildMessage_PlainText(t *testing.T) {
	cfg := &Config{
		Name: "test",
		From: "sender@example.com",
	}
	conn := NewSMTPConnector("test", cfg)

	email := &Email{
		To:       []Recipient{{Email: "user@example.com", Name: "User"}},
		Subject:  "Test Subject",
		TextBody: "Hello, World!",
	}

	msg, err := conn.buildMessage(email)
	if err != nil {
		t.Fatalf("buildMessage failed: %v", err)
	}

	msgStr := string(msg)

	// Check headers
	if !contains(msgStr, "From: sender@example.com") {
		t.Error("missing From header")
	}
	if !contains(msgStr, "To: User <user@example.com>") {
		t.Error("missing To header")
	}
	if !contains(msgStr, "Subject: Test Subject") {
		t.Error("missing Subject header")
	}
	if !contains(msgStr, "Content-Type: text/plain") {
		t.Error("missing Content-Type for plain text")
	}
	if !contains(msgStr, "Hello, World!") {
		t.Error("missing body content")
	}
}

func TestBuildMessage_HTML(t *testing.T) {
	cfg := &Config{
		Name: "test",
		From: "sender@example.com",
	}
	conn := NewSMTPConnector("test", cfg)

	email := &Email{
		To:       []Recipient{{Email: "user@example.com"}},
		Subject:  "Test",
		HTMLBody: "<h1>Hello</h1>",
	}

	msg, err := conn.buildMessage(email)
	if err != nil {
		t.Fatalf("buildMessage failed: %v", err)
	}

	msgStr := string(msg)

	if !contains(msgStr, "Content-Type: text/html") {
		t.Error("missing Content-Type for HTML")
	}
	if !contains(msgStr, "<h1>Hello</h1>") {
		t.Error("missing HTML body")
	}
}

func TestBuildMessage_Multipart(t *testing.T) {
	cfg := &Config{
		Name: "test",
		From: "sender@example.com",
	}
	conn := NewSMTPConnector("test", cfg)

	email := &Email{
		To:       []Recipient{{Email: "user@example.com"}},
		Subject:  "Test",
		TextBody: "Plain text",
		HTMLBody: "<h1>HTML</h1>",
	}

	msg, err := conn.buildMessage(email)
	if err != nil {
		t.Fatalf("buildMessage failed: %v", err)
	}

	msgStr := string(msg)

	if !contains(msgStr, "multipart/alternative") {
		t.Error("missing multipart header")
	}
	if !contains(msgStr, "text/plain") {
		t.Error("missing text/plain part")
	}
	if !contains(msgStr, "text/html") {
		t.Error("missing text/html part")
	}
}

func TestBuildMessage_WithCC(t *testing.T) {
	cfg := &Config{
		Name: "test",
		From: "sender@example.com",
	}
	conn := NewSMTPConnector("test", cfg)

	email := &Email{
		To:       []Recipient{{Email: "user@example.com"}},
		CC:       []Recipient{{Email: "cc@example.com", Name: "CC User"}},
		Subject:  "Test",
		TextBody: "Hello",
	}

	msg, err := conn.buildMessage(email)
	if err != nil {
		t.Fatalf("buildMessage failed: %v", err)
	}

	msgStr := string(msg)

	if !contains(msgStr, "Cc: CC User <cc@example.com>") {
		t.Error("missing CC header")
	}
}

func TestBuildMessage_WithReplyTo(t *testing.T) {
	cfg := &Config{
		Name:    "test",
		From:    "sender@example.com",
		ReplyTo: "reply@example.com",
	}
	conn := NewSMTPConnector("test", cfg)

	email := &Email{
		To:       []Recipient{{Email: "user@example.com"}},
		Subject:  "Test",
		TextBody: "Hello",
	}

	msg, err := conn.buildMessage(email)
	if err != nil {
		t.Fatalf("buildMessage failed: %v", err)
	}

	msgStr := string(msg)

	if !contains(msgStr, "Reply-To: reply@example.com") {
		t.Error("missing Reply-To header")
	}
}

func TestBuildMessage_CustomHeaders(t *testing.T) {
	cfg := &Config{
		Name: "test",
		From: "sender@example.com",
	}
	conn := NewSMTPConnector("test", cfg)

	email := &Email{
		To:       []Recipient{{Email: "user@example.com"}},
		Subject:  "Test",
		TextBody: "Hello",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}

	msg, err := conn.buildMessage(email)
	if err != nil {
		t.Fatalf("buildMessage failed: %v", err)
	}

	msgStr := string(msg)

	if !contains(msgStr, "X-Custom-Header: custom-value") {
		t.Error("missing custom header")
	}
}

func TestNewSendGridConnector(t *testing.T) {
	cfg := &Config{
		Name: "test",
		SendGrid: &SendGridConfig{
			APIKey: "test-key",
		},
	}

	conn := NewSendGridConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "email" {
		t.Errorf("expected type 'email', got %s", conn.Type())
	}
}

func TestSendGridConnector_Connect(t *testing.T) {
	cfg := &Config{
		Name: "test",
		SendGrid: &SendGridConfig{
			APIKey: "test-key",
		},
	}

	conn := NewSendGridConnector("test", cfg)
	err := conn.Connect(context.Background())
	if err != nil {
		t.Errorf("Connect should not fail: %v", err)
	}
}

func TestSendGridConnector_ConnectNoAPIKey(t *testing.T) {
	cfg := &Config{
		Name: "test",
		SendGrid: &SendGridConfig{
			APIKey: "",
		},
	}

	conn := NewSendGridConnector("test", cfg)
	err := conn.Connect(context.Background())
	if err == nil {
		t.Error("Connect should fail without API key")
	}
}

func TestNewSESConnector(t *testing.T) {
	cfg := &Config{
		Name: "test",
		SES:  &SESConfig{Region: "us-east-1"},
	}

	conn := NewSESConnector("test", cfg)

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %s", conn.Name())
	}
	if conn.Type() != "email" {
		t.Errorf("expected type 'email', got %s", conn.Type())
	}
}

func TestFactory(t *testing.T) {
	factory := NewFactory()

	if factory.Type() != "email" {
		t.Errorf("expected type 'email', got %s", factory.Type())
	}
}

func TestFactory_CreateSMTP(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"driver": "smtp",
		"host":   "localhost",
		"port":   587,
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, ok := conn.(*SMTPConnector); !ok {
		t.Error("expected SMTPConnector")
	}
}

func TestFactory_CreateSendGrid(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"driver":  "sendgrid",
		"api_key": "test-key",
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, ok := conn.(*SendGridConnector); !ok {
		t.Error("expected SendGridConnector")
	}
}

func TestFactory_CreateSES(t *testing.T) {
	factory := NewFactory()

	config := map[string]interface{}{
		"driver": "ses",
		"region": "us-east-1",
	}

	conn, err := factory.Create("test", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, ok := conn.(*SESConnector); !ok {
		t.Error("expected SESConnector")
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

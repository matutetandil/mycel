package redis

import (
	"context"
	"encoding/json"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Host != "localhost" {
		t.Errorf("expected host 'localhost', got '%s'", cfg.Host)
	}
	if cfg.Port != 6379 {
		t.Errorf("expected port 6379, got %d", cfg.Port)
	}
	if cfg.DB != 0 {
		t.Errorf("expected db 0, got %d", cfg.DB)
	}
	if cfg.Password != "" {
		t.Errorf("expected empty password, got '%s'", cfg.Password)
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("expected no channels, got %v", cfg.Channels)
	}
	if len(cfg.Patterns) != 0 {
		t.Errorf("expected no patterns, got %v", cfg.Patterns)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
	}{
		{
			name:      "valid default config",
			config:    DefaultConfig(),
			expectErr: false,
		},
		{
			name: "missing host",
			config: &Config{
				Host: "",
				Port: 6379,
			},
			expectErr: true,
		},
		{
			name: "invalid port zero",
			config: &Config{
				Host: "localhost",
				Port: 0,
			},
			expectErr: true,
		},
		{
			name: "invalid port too high",
			config: &Config{
				Host: "localhost",
				Port: 70000,
			},
			expectErr: true,
		},
		{
			name: "negative db",
			config: &Config{
				Host: "localhost",
				Port: 6379,
				DB:   -1,
			},
			expectErr: true,
		},
		{
			name: "valid with channels",
			config: &Config{
				Host:     "redis.example.com",
				Port:     6380,
				Password: "secret",
				DB:       2,
				Channels: []string{"events", "notifications"},
			},
			expectErr: false,
		},
		{
			name: "valid with patterns",
			config: &Config{
				Host:     "localhost",
				Port:     6379,
				Patterns: []string{"events.*", "notifications.*"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigAddr(t *testing.T) {
	cfg := &Config{Host: "redis.example.com", Port: 6380}
	expected := "redis.example.com:6380"
	if addr := cfg.Addr(); addr != expected {
		t.Errorf("expected addr '%s', got '%s'", expected, addr)
	}
}

func TestConfigIsSubscriber(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.IsSubscriber() {
		t.Error("expected IsSubscriber() false for default config")
	}

	cfg.Channels = []string{"test"}
	if !cfg.IsSubscriber() {
		t.Error("expected IsSubscriber() true with channels")
	}

	cfg.Channels = nil
	cfg.Patterns = []string{"test.*"}
	if !cfg.IsSubscriber() {
		t.Error("expected IsSubscriber() true with patterns")
	}
}

func TestNewConnector(t *testing.T) {
	cfg := DefaultConfig()
	conn, err := NewConnector("test-redis", cfg, nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	if conn.Name() != "test-redis" {
		t.Errorf("expected name 'test-redis', got '%s'", conn.Name())
	}
	if conn.Type() != "mq" {
		t.Errorf("expected type 'mq', got '%s'", conn.Type())
	}
}

func TestNewConnectorWithInvalidConfig(t *testing.T) {
	cfg := &Config{Host: "", Port: 0}
	_, err := NewConnector("bad", cfg, nil)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

func TestRegisterRoute(t *testing.T) {
	cfg := DefaultConfig()
	conn, err := NewConnector("test", cfg, nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	called := false
	conn.RegisterRoute("events", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		called = true
		return nil, nil
	})

	conn.mu.RLock()
	handler, ok := conn.handlers["events"]
	conn.mu.RUnlock()

	if !ok {
		t.Fatal("expected handler to be registered for 'events'")
	}

	// Invoke to verify it's the same handler
	_, _ = handler(context.Background(), nil)
	if !called {
		t.Error("expected handler to be called")
	}
}

func TestParseMessageJSON(t *testing.T) {
	payload := map[string]interface{}{
		"user_id": "abc-123",
		"action":  "login",
	}
	payloadBytes, _ := json.Marshal(payload)

	msg := &goredis.Message{
		Channel: "events",
		Payload: string(payloadBytes),
	}

	input := ParseMessage(msg)

	if input["_channel"] != "events" {
		t.Errorf("expected _channel 'events', got '%v'", input["_channel"])
	}
	if _, hasPattern := input["_pattern"]; hasPattern {
		t.Error("expected no _pattern for non-pattern message")
	}
	if input["user_id"] != "abc-123" {
		t.Errorf("expected user_id 'abc-123', got '%v'", input["user_id"])
	}
	if input["action"] != "login" {
		t.Errorf("expected action 'login', got '%v'", input["action"])
	}
}

func TestParseMessageWithPattern(t *testing.T) {
	payload := `{"event":"created"}`

	msg := &goredis.Message{
		Channel: "events.users",
		Pattern: "events.*",
		Payload: payload,
	}

	input := ParseMessage(msg)

	if input["_channel"] != "events.users" {
		t.Errorf("expected _channel 'events.users', got '%v'", input["_channel"])
	}
	if input["_pattern"] != "events.*" {
		t.Errorf("expected _pattern 'events.*', got '%v'", input["_pattern"])
	}
	if input["event"] != "created" {
		t.Errorf("expected event 'created', got '%v'", input["event"])
	}
}

func TestParseMessageNonJSON(t *testing.T) {
	msg := &goredis.Message{
		Channel: "raw-channel",
		Payload: "this is not json",
	}

	input := ParseMessage(msg)

	if input["_channel"] != "raw-channel" {
		t.Errorf("expected _channel 'raw-channel', got '%v'", input["_channel"])
	}
	if input["raw"] != "this is not json" {
		t.Errorf("expected raw payload, got '%v'", input["raw"])
	}
}

func TestFindHandler(t *testing.T) {
	cfg := DefaultConfig()
	conn, _ := NewConnector("test", cfg, nil)

	// Register handlers
	exactCalled := false
	patternCalled := false
	wildcardCalled := false

	conn.handlers["events"] = func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		exactCalled = true
		return nil, nil
	}
	conn.handlers["events.*"] = func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		patternCalled = true
		return nil, nil
	}
	conn.handlers["*"] = func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		wildcardCalled = true
		return nil, nil
	}

	// Test exact match (highest priority)
	handler := conn.findHandler("events", "")
	if handler == nil {
		t.Fatal("expected handler for exact match")
	}
	handler(context.Background(), nil)
	if !exactCalled {
		t.Error("expected exact handler to be called")
	}

	// Test pattern match
	handler = conn.findHandler("events.users", "events.*")
	if handler == nil {
		t.Fatal("expected handler for pattern match")
	}
	handler(context.Background(), nil)
	if !patternCalled {
		t.Error("expected pattern handler to be called")
	}

	// Test wildcard match (lowest priority)
	handler = conn.findHandler("unknown-channel", "")
	if handler == nil {
		t.Fatal("expected wildcard handler")
	}
	handler(context.Background(), nil)
	if !wildcardCalled {
		t.Error("expected wildcard handler to be called")
	}

	// Test no handler at all
	conn2, _ := NewConnector("test2", cfg, nil)
	handler = conn2.findHandler("nothing", "")
	if handler != nil {
		t.Error("expected nil handler when no handlers registered")
	}
}

func TestConnectorStartWithoutSubscriptions(t *testing.T) {
	cfg := DefaultConfig()
	// No channels or patterns - should be a no-op
	conn, _ := NewConnector("test", cfg, nil)

	// Start without connecting (no subscriptions needed)
	// We need a client to avoid nil panic, but since IsSubscriber() is false,
	// Start should return nil immediately
	err := conn.Start(context.Background())
	if err != nil {
		t.Errorf("expected no error for start without subscriptions, got: %v", err)
	}
}

func TestConnectorStartAlreadyRunning(t *testing.T) {
	cfg := DefaultConfig()
	conn, _ := NewConnector("test", cfg, nil)

	// Simulate already running
	conn.running = true

	err := conn.Start(context.Background())
	if err == nil {
		t.Error("expected error for already running connector")
	}
}

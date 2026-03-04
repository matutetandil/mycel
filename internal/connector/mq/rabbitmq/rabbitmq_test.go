package rabbitmq

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Host != "localhost" {
		t.Errorf("expected host 'localhost', got '%s'", cfg.Host)
	}
	if cfg.Port != 5672 {
		t.Errorf("expected port 5672, got %d", cfg.Port)
	}
	if cfg.Username != "guest" {
		t.Errorf("expected username 'guest', got '%s'", cfg.Username)
	}
	if cfg.Password != "guest" {
		t.Errorf("expected password 'guest', got '%s'", cfg.Password)
	}
	if cfg.Vhost != "/" {
		t.Errorf("expected vhost '/', got '%s'", cfg.Vhost)
	}
	if cfg.Heartbeat != 10*time.Second {
		t.Errorf("expected heartbeat 10s, got %v", cfg.Heartbeat)
	}
}

func TestDefaultQueueConfig(t *testing.T) {
	cfg := DefaultQueueConfig()

	if !cfg.Durable {
		t.Error("expected durable to be true")
	}
	if cfg.AutoDelete {
		t.Error("expected auto_delete to be false")
	}
	if cfg.Exclusive {
		t.Error("expected exclusive to be false")
	}
}

func TestDefaultExchangeConfig(t *testing.T) {
	cfg := DefaultExchangeConfig()

	if cfg.Type != ExchangeDirect {
		t.Errorf("expected type 'direct', got '%s'", cfg.Type)
	}
	if !cfg.Durable {
		t.Error("expected durable to be true")
	}
}

func TestDefaultConsumerConfig(t *testing.T) {
	cfg := DefaultConsumerConfig()

	if cfg.AutoAck {
		t.Error("expected auto_ack to be false")
	}
	if cfg.Concurrency != 1 {
		t.Errorf("expected concurrency 1, got %d", cfg.Concurrency)
	}
	if cfg.Prefetch != 10 {
		t.Errorf("expected prefetch 10, got %d", cfg.Prefetch)
	}
}

func TestDefaultPublisherConfig(t *testing.T) {
	cfg := DefaultPublisherConfig()

	if !cfg.Persistent {
		t.Error("expected persistent to be true")
	}
	if cfg.ContentType != "application/json" {
		t.Errorf("expected content_type 'application/json', got '%s'", cfg.ContentType)
	}
}

func TestConfigAMQPURL(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name:     "default config",
			config:   DefaultConfig(),
			expected: "amqp://guest:guest@localhost:5672/",
		},
		{
			name: "custom config",
			config: &Config{
				Host:     "rabbit.example.com",
				Port:     5673,
				Username: "myuser",
				Password: "mypass",
				Vhost:    "/prod",
			},
			expected: "amqp://myuser:mypass@rabbit.example.com:5673/prod",
		},
		{
			name: "with TLS",
			config: &Config{
				Host:     "rabbit.example.com",
				Port:     5671,
				Username: "secure",
				Password: "secret",
				Vhost:    "/",
				TLS:      &TLSConfig{Enabled: true},
			},
			expected: "amqps://secure:secret@rabbit.example.com:5671/",
		},
		{
			name: "vhost without leading slash",
			config: &Config{
				Host:     "rabbit.example.com",
				Port:     5672,
				Username: "api",
				Password: "api",
				Vhost:    "dev",
			},
			expected: "amqp://api:api@rabbit.example.com:5672/dev",
		},
		{
			name: "url overrides fields",
			config: &Config{
				URL:      "amqp://admin:pass@custom-host:5999/myvhost",
				Host:     "ignored",
				Port:     1234,
				Username: "ignored",
				Password: "ignored",
				Vhost:    "/ignored",
			},
			expected: "amqp://admin:pass@custom-host:5999/myvhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.AMQPURL()
			if got != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, got)
			}
		})
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
				Port:     5672,
				Username: "guest",
			},
			expectErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Host:     "localhost",
				Port:     0,
				Username: "guest",
			},
			expectErr: true,
		},
		{
			name: "missing username",
			config: &Config{
				Host: "localhost",
				Port: 5672,
			},
			expectErr: true,
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

func TestConfigIsConsumer(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.IsConsumer() {
		t.Error("expected IsConsumer() to be false for default config")
	}

	cfg.Queue = DefaultQueueConfig()
	if !cfg.IsConsumer() {
		t.Error("expected IsConsumer() to be true when Queue is set")
	}
}

func TestConfigIsPublisher(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.IsPublisher() {
		t.Error("expected IsPublisher() to be false for default config")
	}

	cfg.Publisher = DefaultPublisherConfig()
	if !cfg.IsPublisher() {
		t.Error("expected IsPublisher() to be true when Publisher is set")
	}
}

// Test routing key pattern matching
func TestMatchRoutingKey(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		routingKey string
		expected   bool
	}{
		// Exact matches
		{"exact match", "orders.created", "orders.created", true},
		{"exact no match", "orders.created", "orders.updated", false},

		// Wildcard * (matches one word)
		{"star matches one word", "orders.*", "orders.created", true},
		{"star matches one word 2", "orders.*", "orders.updated", true},
		{"star no match two words", "orders.*", "orders.created.urgent", false},
		{"star in middle", "*.created", "orders.created", true},
		{"star in middle no match", "*.created", "orders.updated", false},

		// Wildcard # (matches zero or more words)
		{"hash matches all", "#", "orders.created.urgent", true},
		{"hash at end", "orders.#", "orders.created", true},
		{"hash at end multi", "orders.#", "orders.created.urgent", true},
		{"hash at end zero words", "orders.#", "orders", true},
		{"hash no match prefix", "orders.#", "products.created", false},

		// Combined patterns
		{"star and hash", "*.created.#", "orders.created", true},
		{"star and hash multi", "*.created.#", "orders.created.urgent.now", true},
		{"complex pattern", "orders.*.#", "orders.created", true},
		{"complex pattern multi", "orders.*.#", "orders.created.urgent", true},

		// Edge cases
		{"empty key", "orders.*", "", false},
		{"empty pattern", "", "orders.created", false},
		{"single star", "*", "orders", true},
		{"single hash", "#", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchRoutingKey(tt.pattern, tt.routingKey)
			if got != tt.expected {
				t.Errorf("matchRoutingKey(%q, %q) = %v, want %v",
					tt.pattern, tt.routingKey, got, tt.expected)
			}
		})
	}
}

func TestSplitRoutingKey(t *testing.T) {
	tests := []struct {
		key      string
		expected []string
	}{
		{"orders.created", []string{"orders", "created"}},
		{"orders.created.urgent", []string{"orders", "created", "urgent"}},
		{"orders", []string{"orders"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := splitRoutingKey(tt.key)
			if len(got) != len(tt.expected) {
				t.Errorf("splitRoutingKey(%q) returned %d parts, want %d",
					tt.key, len(got), len(tt.expected))
				return
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("splitRoutingKey(%q)[%d] = %q, want %q",
						tt.key, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestExchangeTypes(t *testing.T) {
	tests := []struct {
		exchType ExchangeType
		expected string
	}{
		{ExchangeDirect, "direct"},
		{ExchangeFanout, "fanout"},
		{ExchangeTopic, "topic"},
		{ExchangeHeaders, "headers"},
	}

	for _, tt := range tests {
		t.Run(string(tt.exchType), func(t *testing.T) {
			if string(tt.exchType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.exchType))
			}
		})
	}
}

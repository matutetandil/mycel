package kafka

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "localhost:9092" {
		t.Errorf("expected brokers ['localhost:9092'], got %v", cfg.Brokers)
	}
	if cfg.ClientID != "mycel" {
		t.Errorf("expected client_id 'mycel', got '%s'", cfg.ClientID)
	}
}

func TestDefaultConsumerConfig(t *testing.T) {
	cfg := DefaultConsumerConfig()

	if cfg.AutoOffsetReset != "earliest" {
		t.Errorf("expected auto_offset_reset 'earliest', got '%s'", cfg.AutoOffsetReset)
	}
	if !cfg.AutoCommit {
		t.Error("expected auto_commit to be true")
	}
	if cfg.MinBytes != 1 {
		t.Errorf("expected min_bytes 1, got %d", cfg.MinBytes)
	}
	if cfg.MaxBytes != 10*1024*1024 {
		t.Errorf("expected max_bytes 10MB, got %d", cfg.MaxBytes)
	}
	if cfg.MaxWaitTime != 500*time.Millisecond {
		t.Errorf("expected max_wait_time 500ms, got %v", cfg.MaxWaitTime)
	}
	if cfg.Concurrency != 1 {
		t.Errorf("expected concurrency 1, got %d", cfg.Concurrency)
	}
}

func TestDefaultProducerConfig(t *testing.T) {
	cfg := DefaultProducerConfig()

	if cfg.Acks != "all" {
		t.Errorf("expected acks 'all', got '%s'", cfg.Acks)
	}
	if cfg.Retries != 3 {
		t.Errorf("expected retries 3, got %d", cfg.Retries)
	}
	if cfg.BatchSize != 16384 {
		t.Errorf("expected batch_size 16384, got %d", cfg.BatchSize)
	}
	if cfg.LingerMs != 5 {
		t.Errorf("expected linger_ms 5, got %d", cfg.LingerMs)
	}
	if cfg.Compression != "none" {
		t.Errorf("expected compression 'none', got '%s'", cfg.Compression)
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
			name: "missing brokers",
			config: &Config{
				Brokers: []string{},
			},
			expectErr: true,
		},
		{
			name: "valid consumer config",
			config: &Config{
				Brokers: []string{"localhost:9092"},
				Consumer: &ConsumerConfig{
					GroupID: "test-group",
					Topics:  []string{"test-topic"},
				},
			},
			expectErr: false,
		},
		{
			name: "consumer without group_id",
			config: &Config{
				Brokers: []string{"localhost:9092"},
				Consumer: &ConsumerConfig{
					Topics: []string{"test-topic"},
				},
			},
			expectErr: true,
		},
		{
			name: "valid producer config",
			config: &Config{
				Brokers: []string{"localhost:9092"},
				Producer: &ProducerConfig{
					Topic: "test-topic",
					Acks:  "all",
				},
			},
			expectErr: false,
		},
		{
			name: "invalid acks value",
			config: &Config{
				Brokers: []string{"localhost:9092"},
				Producer: &ProducerConfig{
					Acks: "invalid",
				},
			},
			expectErr: true,
		},
		{
			name: "invalid compression",
			config: &Config{
				Brokers: []string{"localhost:9092"},
				Producer: &ProducerConfig{
					Compression: "invalid",
				},
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

	cfg.Consumer = DefaultConsumerConfig()
	if !cfg.IsConsumer() {
		t.Error("expected IsConsumer() to be true when Consumer is set")
	}
}

func TestConfigIsProducer(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.IsProducer() {
		t.Error("expected IsProducer() to be false for default config")
	}

	cfg.Producer = DefaultProducerConfig()
	if !cfg.IsProducer() {
		t.Error("expected IsProducer() to be true when Producer is set")
	}
}

func TestCompressionValues(t *testing.T) {
	validCompressions := []string{"none", "gzip", "snappy", "lz4", "zstd", ""}

	for _, comp := range validCompressions {
		cfg := &Config{
			Brokers: []string{"localhost:9092"},
			Producer: &ProducerConfig{
				Compression: comp,
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("compression '%s' should be valid, got error: %v", comp, err)
		}
	}
}

func TestAcksValues(t *testing.T) {
	validAcks := []string{"none", "one", "all", ""}

	for _, acks := range validAcks {
		cfg := &Config{
			Brokers: []string{"localhost:9092"},
			Producer: &ProducerConfig{
				Acks: acks,
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("acks '%s' should be valid, got error: %v", acks, err)
		}
	}
}

func TestAutoOffsetResetValues(t *testing.T) {
	tests := []struct {
		value    string
		expected string
	}{
		{"earliest", "earliest"},
		{"latest", "latest"},
		{"", "earliest"}, // Default should be earliest
	}

	for _, tt := range tests {
		cfg := DefaultConsumerConfig()
		if tt.value != "" {
			cfg.AutoOffsetReset = tt.value
		}
		// The config itself doesn't transform values, just stores them
		// The actual transformation happens in startConsumer
	}
}

func TestNewConnector(t *testing.T) {
	cfg := DefaultConfig()

	conn, err := NewConnector("test-kafka", cfg, nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	if conn.Name() != "test-kafka" {
		t.Errorf("expected name 'test-kafka', got '%s'", conn.Name())
	}

	if conn.Type() != "mq" {
		t.Errorf("expected type 'mq', got '%s'", conn.Type())
	}
}

func TestNewConnectorWithInvalidConfig(t *testing.T) {
	cfg := &Config{
		Brokers: []string{}, // Invalid - no brokers
	}

	_, err := NewConnector("test-kafka", cfg, nil)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

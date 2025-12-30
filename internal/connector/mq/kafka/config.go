package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"
)

// Config holds the Kafka connector configuration.
type Config struct {
	// Broker addresses
	Brokers []string

	// Client identifier
	ClientID string

	// Security
	SASL *SASLConfig
	TLS  *TLSConfig

	// Consumer settings
	Consumer *ConsumerConfig

	// Producer settings
	Producer *ProducerConfig
}

// SASLConfig holds SASL authentication configuration.
type SASLConfig struct {
	Mechanism string // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Username  string
	Password  string
}

// TLSConfig holds TLS configuration options.
type TLSConfig struct {
	Enabled            bool
	CertFile           string
	KeyFile            string
	CAFile             string
	InsecureSkipVerify bool
}

// ConsumerConfig holds consumer-specific configuration.
type ConsumerConfig struct {
	// Consumer group ID (required for group consumption)
	GroupID string

	// Topics to consume from
	Topics []string

	// Offset management
	AutoOffsetReset string // earliest, latest
	AutoCommit      bool

	// Performance tuning
	MinBytes    int           // Minimum bytes to fetch
	MaxBytes    int           // Maximum bytes to fetch
	MaxWaitTime time.Duration // Maximum time to wait for new data

	// Concurrency
	Concurrency int
}

// ProducerConfig holds producer-specific configuration.
type ProducerConfig struct {
	// Default topic for publishing
	Topic string

	// Delivery guarantees
	Acks    string // none, one, all
	Retries int

	// Batching
	BatchSize int // Maximum batch size in bytes
	LingerMs  int // Time to wait for batch to fill

	// Compression
	Compression string // none, gzip, snappy, lz4, zstd
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Brokers:  []string{"localhost:9092"},
		ClientID: "mycel",
	}
}

// DefaultConsumerConfig returns default consumer configuration.
func DefaultConsumerConfig() *ConsumerConfig {
	return &ConsumerConfig{
		AutoOffsetReset: "earliest",
		AutoCommit:      true,
		MinBytes:        1,
		MaxBytes:        10 * 1024 * 1024, // 10MB
		MaxWaitTime:     500 * time.Millisecond,
		Concurrency:     1,
	}
}

// DefaultProducerConfig returns default producer configuration.
func DefaultProducerConfig() *ProducerConfig {
	return &ProducerConfig{
		Acks:        "all",
		Retries:     3,
		BatchSize:   16384,
		LingerMs:    5,
		Compression: "none",
	}
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("at least one broker is required")
	}

	// Validate consumer config
	if c.Consumer != nil {
		if c.Consumer.GroupID == "" && len(c.Consumer.Topics) > 0 {
			return fmt.Errorf("consumer group_id is required when topics are specified")
		}
	}

	// Validate producer config
	if c.Producer != nil {
		switch c.Producer.Acks {
		case "none", "one", "all", "":
			// Valid
		default:
			return fmt.Errorf("invalid acks value: %s (must be none, one, or all)", c.Producer.Acks)
		}

		switch c.Producer.Compression {
		case "none", "gzip", "snappy", "lz4", "zstd", "":
			// Valid
		default:
			return fmt.Errorf("invalid compression: %s", c.Producer.Compression)
		}
	}

	return nil
}

// IsConsumer returns true if this config is for a consumer.
func (c *Config) IsConsumer() bool {
	return c.Consumer != nil
}

// IsProducer returns true if this config is for a producer.
func (c *Config) IsProducer() bool {
	return c.Producer != nil
}

// BuildTLSConfig creates a *tls.Config from the TLSConfig.
func (c *TLSConfig) BuildTLSConfig() (*tls.Config, error) {
	if c == nil || !c.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	// Load CA certificate if provided
	if c.CAFile != "" {
		caCert, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Load client certificate if provided
	if c.CertFile != "" && c.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

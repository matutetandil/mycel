package rabbitmq

import (
	"crypto/tls"
	"fmt"
	"time"
)

// ExchangeType represents the type of AMQP exchange.
type ExchangeType string

const (
	ExchangeDirect  ExchangeType = "direct"
	ExchangeFanout  ExchangeType = "fanout"
	ExchangeTopic   ExchangeType = "topic"
	ExchangeHeaders ExchangeType = "headers"
)

// Config holds the RabbitMQ connector configuration.
type Config struct {
	// Connection settings
	Host     string
	Port     int
	Username string
	Password string
	Vhost    string

	// TLS configuration
	TLS *TLSConfig

	// Queue configuration (for consumers)
	Queue *QueueConfig

	// Exchange configuration
	Exchange *ExchangeConfig

	// Consumer configuration
	Consumer *ConsumerConfig

	// Publisher configuration
	Publisher *PublisherConfig

	// Connection settings
	Heartbeat       time.Duration
	ConnectionName  string
	ReconnectDelay  time.Duration
	MaxReconnects   int
}

// TLSConfig holds TLS configuration options.
type TLSConfig struct {
	Enabled            bool
	CertFile           string
	KeyFile            string
	CAFile             string
	InsecureSkipVerify bool
}

// QueueConfig holds queue declaration options.
type QueueConfig struct {
	Name       string
	Durable    bool
	AutoDelete bool
	Exclusive  bool
	NoWait     bool
	Args       map[string]interface{}
}

// ExchangeConfig holds exchange declaration and binding options.
type ExchangeConfig struct {
	Name       string
	Type       ExchangeType
	Durable    bool
	AutoDelete bool
	Internal   bool
	NoWait     bool
	Args       map[string]interface{}

	// Binding configuration
	RoutingKey string
	BindArgs   map[string]interface{}
}

// ConsumerConfig holds consumer options.
type ConsumerConfig struct {
	Tag         string
	AutoAck     bool
	Exclusive   bool
	NoLocal     bool
	NoWait      bool
	Concurrency int
	Prefetch    int
	Args        map[string]interface{}

	// Dead Letter Queue configuration
	DLQ *DLQConfig
}

// DLQConfig holds Dead Letter Queue configuration.
type DLQConfig struct {
	// Enabled controls whether DLQ processing is enabled
	Enabled bool

	// Exchange is the DLQ exchange name (default: <main-exchange>.dlx)
	Exchange string

	// Queue is the DLQ queue name (default: <main-queue>.dlq)
	Queue string

	// RoutingKey is the routing key for DLQ messages
	RoutingKey string

	// MaxRetries is the maximum number of retry attempts before routing to DLQ
	// Default: 3
	MaxRetries int

	// RetryDelay is the delay before requeuing a message for retry
	// If set, messages are delayed using RabbitMQ's delayed message plugin
	// or a TTL-based approach
	RetryDelay time.Duration

	// RetryHeader is the header name used to track retry count
	// Default: x-retry-count
	RetryHeader string
}

// PublisherConfig holds publisher options.
type PublisherConfig struct {
	Exchange    string
	RoutingKey  string
	Mandatory   bool
	Immediate   bool
	Persistent  bool
	ContentType string
	Confirms    bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Host:           "localhost",
		Port:           5672,
		Username:       "guest",
		Password:       "guest",
		Vhost:          "/",
		Heartbeat:      10 * time.Second,
		ReconnectDelay: 5 * time.Second,
		MaxReconnects:  10,
	}
}

// DefaultQueueConfig returns default queue configuration.
func DefaultQueueConfig() *QueueConfig {
	return &QueueConfig{
		Durable:    true,
		AutoDelete: false,
		Exclusive:  false,
		NoWait:     false,
	}
}

// DefaultExchangeConfig returns default exchange configuration.
func DefaultExchangeConfig() *ExchangeConfig {
	return &ExchangeConfig{
		Type:       ExchangeDirect,
		Durable:    true,
		AutoDelete: false,
		Internal:   false,
		NoWait:     false,
	}
}

// DefaultConsumerConfig returns default consumer configuration.
func DefaultConsumerConfig() *ConsumerConfig {
	return &ConsumerConfig{
		AutoAck:     false,
		Exclusive:   false,
		NoLocal:     false,
		NoWait:      false,
		Concurrency: 1,
		Prefetch:    10,
	}
}

// DefaultDLQConfig returns default DLQ configuration.
func DefaultDLQConfig() *DLQConfig {
	return &DLQConfig{
		Enabled:     true,
		MaxRetries:  3,
		RetryHeader: "x-retry-count",
	}
}

// DefaultPublisherConfig returns default publisher configuration.
func DefaultPublisherConfig() *PublisherConfig {
	return &PublisherConfig{
		Mandatory:   false,
		Immediate:   false,
		Persistent:  true,
		ContentType: "application/json",
		Confirms:    false,
	}
}

// AMQPURL returns the AMQP connection URL.
func (c *Config) AMQPURL() string {
	scheme := "amqp"
	if c.TLS != nil && c.TLS.Enabled {
		scheme = "amqps"
	}
	return fmt.Sprintf("%s://%s:%s@%s:%d%s",
		scheme,
		c.Username,
		c.Password,
		c.Host,
		c.Port,
		c.Vhost,
	)
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

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	if c.Username == "" {
		return fmt.Errorf("username is required")
	}

	return nil
}

// IsConsumer returns true if this config is for a consumer.
func (c *Config) IsConsumer() bool {
	return c.Consumer != nil || c.Queue != nil
}

// IsPublisher returns true if this config is for a publisher.
func (c *Config) IsPublisher() bool {
	return c.Publisher != nil
}

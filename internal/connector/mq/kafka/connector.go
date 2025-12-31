package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/segmentio/kafka-go"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/mq/types"
)

// HandlerFunc is the function signature for message handlers.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Connector is a Kafka connector that supports both consuming and producing.
type Connector struct {
	name   string
	config *Config
	logger *slog.Logger

	// Reader for consuming (one per consumer group)
	reader *kafka.Reader

	// Writer for producing
	writer *kafka.Writer

	// Handler registration
	handlers map[string]HandlerFunc // topic -> handler
	mu       sync.RWMutex

	// Lifecycle
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewConnector creates a new Kafka connector.
func NewConnector(name string, config *Config, logger *slog.Logger) (*Connector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Connector{
		name:     name,
		config:   config,
		logger:   logger.With("connector", name, "type", "kafka"),
		handlers: make(map[string]HandlerFunc),
	}, nil
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "mq"
}

// Connect establishes connection to Kafka brokers.
func (c *Connector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create context for lifecycle management
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Build TLS config if needed
	var tlsConfig = c.config.TLS
	if tlsConfig != nil && tlsConfig.Enabled {
		_, err := tlsConfig.BuildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
	}

	// Initialize producer if configured
	if c.config.IsProducer() {
		if err := c.initProducer(); err != nil {
			return fmt.Errorf("failed to initialize producer: %w", err)
		}
	}

	c.logger.Info("connected to Kafka",
		"brokers", c.config.Brokers,
		"is_consumer", c.config.IsConsumer(),
		"is_producer", c.config.IsProducer(),
	)

	return nil
}

// Close closes the connection to Kafka.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Wait for consumers to finish
	c.wg.Wait()

	var errs []error

	// Close reader
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close reader: %w", err))
		}
		c.reader = nil
	}

	// Close writer
	if c.writer != nil {
		if err := c.writer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close writer: %w", err))
		}
		c.writer = nil
	}

	c.running = false

	if len(errs) > 0 {
		return errs[0]
	}

	c.logger.Info("disconnected from Kafka")
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try to dial a broker to verify connectivity
	for _, broker := range c.config.Brokers {
		conn, err := kafka.DialContext(ctx, "tcp", broker)
		if err != nil {
			continue
		}
		conn.Close()
		return nil
	}

	return fmt.Errorf("unable to connect to any broker")
}

// Start starts the consumer if configured.
func (c *Connector) Start(ctx context.Context) error {
	if !c.config.IsConsumer() {
		return nil
	}

	return c.startConsumer(ctx)
}

// RegisterHandler registers a handler for a specific topic pattern.
func (c *Connector) RegisterHandler(pattern string, handler HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[pattern] = handler
}

// RegisterRoute implements runtime.RouteRegistrar for flow integration.
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.RegisterHandler(operation, handler)
}

// Read implements connector.Reader (not typically used for Kafka consumers).
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// For Kafka, reading is typically done via the consumer loop
	// This method could be used for one-off reads if needed
	return nil, fmt.Errorf("use consumer mode for reading from Kafka")
}

// Write implements connector.Writer for publishing messages.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Create message from payload
	msg := types.NewMessage(data.Payload)

	// Set topic from target
	if data.Target != "" {
		msg.RoutingKey = data.Target // Use RoutingKey as topic
	}

	// Publish the message
	if err := c.Publish(ctx, msg); err != nil {
		return nil, err
	}

	return &connector.Result{
		Rows:     []map[string]interface{}{data.Payload},
		Affected: 1,
	}, nil
}

// initProducer initializes the Kafka writer.
func (c *Connector) initProducer() error {
	producerCfg := c.config.Producer
	if producerCfg == nil {
		producerCfg = DefaultProducerConfig()
	}

	// Map compression setting
	var compression kafka.Compression
	switch producerCfg.Compression {
	case "gzip":
		compression = kafka.Gzip
	case "snappy":
		compression = kafka.Snappy
	case "lz4":
		compression = kafka.Lz4
	case "zstd":
		compression = kafka.Zstd
	default:
		compression = 0 // No compression
	}

	// Map acks setting
	var requiredAcks kafka.RequiredAcks
	switch producerCfg.Acks {
	case "none":
		requiredAcks = kafka.RequireNone
	case "one":
		requiredAcks = kafka.RequireOne
	case "all", "":
		requiredAcks = kafka.RequireAll
	}

	c.writer = &kafka.Writer{
		Addr:         kafka.TCP(c.config.Brokers...),
		Topic:        producerCfg.Topic,
		Balancer:     &kafka.LeastBytes{},
		MaxAttempts:  producerCfg.Retries,
		BatchSize:    producerCfg.BatchSize,
		RequiredAcks: requiredAcks,
		Compression:  compression,
	}

	return nil
}

// findHandler finds a handler for the given topic.
func (c *Connector) findHandler(topic string) HandlerFunc {
	// Try exact match first
	if handler, ok := c.handlers[topic]; ok {
		return handler
	}

	// Try wildcard handler
	if handler, ok := c.handlers["*"]; ok {
		return handler
	}

	return nil
}

// buildKafkaMessage converts a types.Message to a kafka.Message.
func (c *Connector) buildKafkaMessage(msg *types.Message) (kafka.Message, error) {
	// Serialize body
	body, err := json.Marshal(msg.Body)
	if err != nil {
		return kafka.Message{}, fmt.Errorf("failed to serialize message body: %w", err)
	}

	// Build headers
	var headers []kafka.Header
	for k, v := range msg.Headers {
		headers = append(headers, kafka.Header{Key: k, Value: []byte(v)})
	}

	// Add message ID as header if present
	if msg.ID != "" {
		headers = append(headers, kafka.Header{Key: "message-id", Value: []byte(msg.ID)})
	}

	return kafka.Message{
		Topic:   msg.RoutingKey, // In Kafka, we use RoutingKey as topic
		Key:     []byte(msg.ID),
		Value:   body,
		Headers: headers,
	}, nil
}

package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
	"github.com/matutetandil/mycel/v3/internal/connector/mq/undispatched"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/tracing"
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

	// Schema Registry client
	schemaRegistry *SchemaRegistryClient

	// Handler registration
	handlers map[string]HandlerFunc // topic -> handler
	mu       sync.RWMutex

	// Lifecycle
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Filter rejection tracking for requeue dedup
	requeueTracker *flow.RequeueTracker

	// Debug throttling: single-message processing when debugger is connected
	debugGate connector.DebugGate

	// Reports messages no flow handler matched.
	undispatched undispatched.Reporter
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

	// A schema registry that nothing serialises through is worse than none.
	//
	// The client was built here, logged "schema registry enabled", and was
	// never called: its three methods have no call sites, and messages are
	// encoded and decoded by the ordinary JSON codec whatever the block says.
	// So a connector configured for Avro put JSON on the topic, and every
	// consumer expecting the registry's wire format — the magic byte and the
	// schema id — read something else.
	//
	// Refused rather than accepted, until there is a serialiser behind it.
	if c.config.SchemaRegistry != nil {
		return fmt.Errorf("connector %q: a schema_registry block is configured, and nothing is serialised through it yet — messages are encoded as JSON whatever it says, so it is refused rather than quietly ignored", c.name)
	}

	// Initialize producer if configured
	if c.config.IsProducer() {
		if err := c.initProducer(); err != nil {
			return fmt.Errorf("failed to initialize producer: %w", err)
		}
	}

	attrs := []any{
		"brokers", c.config.Brokers,
		"is_consumer", c.config.IsConsumer(),
		"is_producer", c.config.IsProducer(),
	}
	if c.config.Consumer != nil {
		if len(c.config.Consumer.Topics) > 0 {
			attrs = append(attrs, "topics", c.config.Consumer.Topics)
		}
		if c.config.Consumer.GroupID != "" {
			attrs = append(attrs, "consumer_group", c.config.Consumer.GroupID)
		}
	}
	if c.config.Producer != nil && c.config.Producer.Topic != "" {
		attrs = append(attrs, "topic", c.config.Producer.Topic)
	}
	c.logger.Info("connected to Kafka", attrs...)

	return nil
}

// Close closes the connection to Kafka.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Wait for consumers to finish, but not forever: a consumer blocked on a
	// broker that has stopped answering must not hold the whole process open.
	// Same bound the RabbitMQ connector already applies.
	consumersDone := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(consumersDone)
	}()
	select {
	case <-consumersDone:
	case <-time.After(5 * time.Second):
		c.logger.Warn("timeout waiting for consumers to finish")
	case <-ctx.Done():
	}

	// Stop requeue tracker
	if c.requeueTracker != nil {
		c.requeueTracker.Stop()
		c.requeueTracker = nil
	}

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

// GetSchemaRegistry returns the Schema Registry client (may be nil if not configured).
func (c *Connector) GetSchemaRegistry() *SchemaRegistryClient {
	return c.schemaRegistry
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

	c.requeueTracker = flow.NewRequeueTracker(10 * time.Minute)

	return c.startConsumer(ctx)
}

// AllowOne permits exactly one message through the debug gate.
// Called when the IDE sends debug.consume.
func (c *Connector) AllowOne() {
	c.debugGate.Allow()
}

// SourceInfo returns the connector type and topic info for IDE display.
func (c *Connector) SourceInfo() (string, string) {
	if c.config.Consumer != nil && len(c.config.Consumer.Topics) > 0 {
		return "kafka", strings.Join(c.config.Consumer.Topics, ",")
	}
	return "kafka", ""
}

// RegisterHandler registers a handler for a specific topic pattern.
func (c *Connector) RegisterHandler(pattern string, handler HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.handlers[pattern]; ok {
		c.handlers[pattern] = HandlerFunc(connector.ChainEventDriven(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "topic", pattern)
	} else {
		c.handlers[pattern] = handler
	}
}

// SetDebugMode enables or disables single-message debug throttling.
func (c *Connector) SetDebugMode(enabled bool) {
	c.debugGate.SetEnabled(enabled)
	if enabled {
		c.logger.Info("debug mode enabled: single-message processing", "name", c.name)
	} else {
		c.logger.Info("debug mode disabled: concurrent processing restored", "name", c.name)
	}
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

	// Propagate the active distributed trace into the Kafka record headers so a
	// downstream consumer continues the same trace (no-op when tracing is off).
	msg.Headers = tracing.InjectInto(ctx, msg.Headers)

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

	// How long the writer waits for a batch to fill before sending what it
	// has. linger_ms was read from the configuration into a field nothing
	// used, so the library's own default applied — a full second — and a
	// producer sending one message per request waited that long for each,
	// whatever the configuration said.
	batchTimeout := time.Duration(producerCfg.LingerMs) * time.Millisecond
	if batchTimeout <= 0 {
		// Zero would be read as "unset" by the library and turn into the
		// default again; the smallest wait is what somebody asking for no
		// batching means.
		batchTimeout = time.Millisecond
	}

	// The topic goes on each message, never on the writer.
	//
	// kafka-go refuses a message that carries a topic when the writer has one
	// — "Topic must not be specified for both Writer and Message" — and this
	// connector always puts one on the message, falling back to
	// producer.topic when the flow names none. So configuring producer.topic,
	// which the documentation lists as required, made every publish through
	// that connector fail with a message about the library's own rules. The
	// integration configuration carried a comment working around it.
	c.writer = &kafka.Writer{
		Addr:         kafka.TCP(c.config.Brokers...),
		Balancer:     &kafka.LeastBytes{},
		MaxAttempts:  producerCfg.Retries,
		BatchSize:    producerCfg.BatchSize,
		BatchTimeout: batchTimeout,
		RequiredAcks: requiredAcks,
		Compression:  compression,
	}

	// Configure Transport for TLS/SASL if needed
	if c.config.TLS != nil || c.config.SASL != nil {
		transport := &kafka.Transport{}

		// TLS configuration
		if c.config.TLS != nil && c.config.TLS.Enabled {
			tlsConfig, err := c.config.TLS.BuildTLSConfig()
			if err != nil {
				return fmt.Errorf("failed to build TLS config for producer: %w", err)
			}
			transport.TLS = tlsConfig
		}

		// SASL configuration
		if c.config.SASL != nil {
			mechanism, err := c.buildSASLMechanism()
			if err != nil {
				return fmt.Errorf("failed to build SASL mechanism for producer: %w", err)
			}
			transport.SASL = mechanism
		}

		c.writer.Transport = transport
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

// Package mqtt implements an MQTT connector for Mycel.
// It supports publishing messages, subscribing to topics, and processing
// incoming messages through registered handlers.
package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/matutetandil/mycel/internal/connector"
)

// HandlerFunc is the function signature for message handlers.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Config holds MQTT connector configuration.
type Config struct {
	// Broker is the MQTT broker URL (e.g., tcp://localhost:1883, ssl://broker:8883).
	Broker string

	// ClientID identifies this client to the broker.
	ClientID string

	// Username for broker authentication.
	Username string

	// Password for broker authentication.
	Password string

	// QoS is the default Quality of Service level (0, 1, or 2).
	QoS byte

	// CleanSession controls whether the broker stores subscriptions and pending
	// messages for this client after disconnect.
	CleanSession bool

	// KeepAlive is the interval between PINGREQ packets.
	KeepAlive time.Duration

	// Topic is the default topic for publish operations.
	Topic string

	// TLS holds TLS configuration for secure connections.
	TLS *TLSConfig

	// ConnectTimeout is the maximum time to wait for a connection.
	ConnectTimeout time.Duration

	// AutoReconnect enables automatic reconnection on disconnect.
	AutoReconnect bool

	// MaxReconnectInterval is the maximum wait time between reconnection attempts.
	MaxReconnectInterval time.Duration
}

// TLSConfig holds TLS configuration options.
type TLSConfig struct {
	Enabled            bool
	CertFile           string
	KeyFile            string
	CAFile             string
	InsecureSkipVerify bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Broker:               "tcp://localhost:1883",
		ClientID:             "mycel",
		QoS:                  0,
		CleanSession:         true,
		KeepAlive:            30 * time.Second,
		ConnectTimeout:       10 * time.Second,
		AutoReconnect:        true,
		MaxReconnectInterval: 5 * time.Minute,
	}
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Broker == "" {
		return fmt.Errorf("broker URL is required")
	}
	if c.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	if c.QoS > 2 {
		return fmt.Errorf("invalid QoS level: %d (must be 0, 1, or 2)", c.QoS)
	}
	return nil
}

// Connector is an MQTT connector that supports both publishing and subscribing.
type Connector struct {
	name   string
	config *Config
	logger *slog.Logger

	// MQTT client
	client pahomqtt.Client
	mu     sync.RWMutex

	// Consumer state
	handlers map[string]HandlerFunc // topic pattern -> handler
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc

	// Debug throttling: single-message processing when debugger is connected
	debugGate connector.DebugGate
}

// NewConnector creates a new MQTT connector.
func NewConnector(name string, config *Config, logger *slog.Logger) (*Connector, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &Connector{
		name:     name,
		config:   config,
		logger:   logger,
		handlers: make(map[string]HandlerFunc),
	}, nil
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns "mqtt".
func (c *Connector) Type() string {
	return "mqtt"
}

// Connect establishes a connection to the MQTT broker.
func (c *Connector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil && c.client.IsConnected() {
		return nil // Already connected
	}

	opts := c.buildClientOptions()

	client := pahomqtt.NewClient(opts)
	token := client.Connect()

	timeout := c.config.ConnectTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	if !token.WaitTimeout(timeout) {
		return fmt.Errorf("connection to MQTT broker timed out")
	}
	if token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	c.client = client
	c.logger.Info("connected to MQTT broker",
		"name", c.name,
		"broker", c.config.Broker,
		"client_id", c.config.ClientID,
	)

	return nil
}

// buildClientOptions creates the paho MQTT client options from config.
func (c *Connector) buildClientOptions() *pahomqtt.ClientOptions {
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(c.config.Broker)
	opts.SetClientID(c.config.ClientID)
	opts.SetCleanSession(c.config.CleanSession)
	opts.SetAutoReconnect(c.config.AutoReconnect)
	opts.SetMaxReconnectInterval(c.config.MaxReconnectInterval)

	if c.config.KeepAlive > 0 {
		opts.SetKeepAlive(c.config.KeepAlive)
	}

	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
	}
	if c.config.Password != "" {
		opts.SetPassword(c.config.Password)
	}

	if c.config.ConnectTimeout > 0 {
		opts.SetConnectTimeout(c.config.ConnectTimeout)
	}

	// TLS configuration
	if c.config.TLS != nil && c.config.TLS.Enabled {
		tlsCfg, err := buildTLSConfig(c.config.TLS)
		if err != nil {
			c.logger.Error("failed to build TLS config", "error", err)
		} else {
			opts.SetTLSConfig(tlsCfg)
		}
	}

	// Connection lost handler
	opts.SetConnectionLostHandler(func(_ pahomqtt.Client, err error) {
		c.logger.Error("MQTT connection lost", "name", c.name, "error", err)
	})

	// Reconnect handler
	opts.SetOnConnectHandler(func(_ pahomqtt.Client) {
		c.logger.Info("MQTT connected/reconnected", "name", c.name)
		// Re-subscribe on reconnect
		c.mu.RLock()
		running := c.running
		c.mu.RUnlock()
		if running {
			c.resubscribe()
		}
	})

	return opts
}

// Close closes the connection to the MQTT broker.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.client != nil && c.client.IsConnected() {
		// Disconnect with a 1-second quiesce period
		c.client.Disconnect(1000)
	}

	c.running = false
	c.logger.Info("disconnected from MQTT broker", "name", c.name)
	return nil
}

// Health checks if the MQTT client is connected.
func (c *Connector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("not connected to MQTT broker")
	}
	return nil
}

// Read implements connector.Reader.
// For MQTT, this is a no-op that returns an empty result since MQTT is
// primarily push-based. Use RegisterRoute + Start for subscriptions.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	return &connector.Result{
		Rows: []map[string]interface{}{},
	}, nil
}

// Write implements connector.Writer for publishing messages.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil || !c.client.IsConnected() {
		return nil, fmt.Errorf("MQTT client is not connected")
	}

	// Determine topic
	topic := data.Target
	if topic == "" {
		topic = c.config.Topic
	}
	if topic == "" {
		return nil, fmt.Errorf("topic is required for publish")
	}

	// Determine QoS
	qos := c.config.QoS
	if q, ok := data.Params["qos"]; ok {
		switch v := q.(type) {
		case int:
			qos = byte(v)
		case float64:
			qos = byte(v)
		case byte:
			qos = v
		}
	}

	// Determine retain flag
	retain := false
	if r, ok := data.Params["retain"].(bool); ok {
		retain = r
	}

	// Serialize payload
	payload, err := json.Marshal(data.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize payload: %w", err)
	}

	token := c.client.Publish(topic, qos, retain, payload)
	token.Wait()
	if token.Error() != nil {
		return nil, fmt.Errorf("failed to publish message: %w", token.Error())
	}

	c.logger.Debug("published MQTT message",
		"topic", topic,
		"qos", qos,
		"retain", retain,
		"size", len(payload),
	)

	return &connector.Result{
		Affected: 1,
		Metadata: map[string]interface{}{
			"topic":  topic,
			"qos":    qos,
			"retain": retain,
		},
	}, nil
}

// RegisterRoute registers a handler for an MQTT topic pattern.
// This implements the RouteRegistrar interface for flow integration.
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.handlers[operation]; ok {
		c.handlers[operation] = HandlerFunc(connector.ChainEventDriven(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "topic", operation)
	} else {
		c.handlers[operation] = handler
	}
	c.logger.Debug("registered MQTT handler", "topic", operation)
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

// Start begins subscribing to topics (implements Starter interface).
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("connector already running")
	}
	c.running = true
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	// Subscribe to all registered topics
	return c.subscribeAll()
}

// subscribeAll subscribes to all registered topic handlers.
func (c *Connector) subscribeAll() error {
	c.mu.RLock()
	handlers := make(map[string]HandlerFunc, len(c.handlers))
	for topic, handler := range c.handlers {
		handlers[topic] = handler
	}
	c.mu.RUnlock()

	for topic, handler := range handlers {
		if err := c.subscribe(topic, handler); err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}
	}

	return nil
}

// subscribe subscribes to a single topic with the given handler.
func (c *Connector) subscribe(topic string, handler HandlerFunc) error {
	c.mu.RLock()
	client := c.client
	qos := c.config.QoS
	c.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	callback := c.buildMessageHandler(topic, handler)

	token := client.Subscribe(topic, qos, callback)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("subscribe failed: %w", token.Error())
	}

	c.logger.Info("subscribed to MQTT topic",
		"name", c.name,
		"topic", topic,
		"qos", qos,
	)

	return nil
}

// buildMessageHandler creates a paho MQTT message handler that dispatches
// messages to the registered HandlerFunc.
func (c *Connector) buildMessageHandler(topic string, handler HandlerFunc) pahomqtt.MessageHandler {
	return func(_ pahomqtt.Client, msg pahomqtt.Message) {
		input := ParseMessage(msg)

		c.mu.RLock()
		ctx := c.ctx
		c.mu.RUnlock()

		if ctx == nil {
			ctx = context.Background()
		}

		// Debug throttling: wait for gate before processing
		c.debugGate.Acquire()
		_, err := handler(ctx, input)
		c.debugGate.Release()

		if err != nil {
			c.logger.Error("MQTT handler error",
				"topic", msg.Topic(),
				"error", err,
			)
		}
	}
}

// resubscribe re-subscribes to all topics after a reconnect.
func (c *Connector) resubscribe() {
	c.mu.RLock()
	handlers := make(map[string]HandlerFunc, len(c.handlers))
	for topic, handler := range c.handlers {
		handlers[topic] = handler
	}
	c.mu.RUnlock()

	for topic, handler := range handlers {
		if err := c.subscribe(topic, handler); err != nil {
			c.logger.Error("failed to resubscribe",
				"topic", topic,
				"error", err,
			)
		}
	}
}

// ParseMessage converts a paho MQTT message into a map suitable for
// flow handler input. Exported for testing.
func ParseMessage(msg pahomqtt.Message) map[string]interface{} {
	input := map[string]interface{}{
		"_topic":      msg.Topic(),
		"_message_id": msg.MessageID(),
		"_qos":        msg.Qos(),
		"_retained":   msg.Retained(),
	}

	// Try to parse payload as JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &parsed); err == nil {
		// Merge parsed fields into input
		for k, v := range parsed {
			input[k] = v
		}
	} else {
		// Not JSON, include raw payload
		input["_raw"] = string(msg.Payload())
	}

	return input
}

// TopicHandlers returns a copy of the registered handlers (for testing).
func (c *Connector) TopicHandlers() map[string]HandlerFunc {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]HandlerFunc, len(c.handlers))
	for k, v := range c.handlers {
		result[k] = v
	}
	return result
}

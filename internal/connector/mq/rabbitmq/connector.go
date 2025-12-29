package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/connector/mq/types"
)

// HandlerFunc is the function signature for message handlers.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Connector is a RabbitMQ connector that supports both consuming and publishing.
type Connector struct {
	name   string
	config *Config
	logger *slog.Logger

	// Connection management
	conn    *amqp.Connection
	channel *amqp.Channel
	mu      sync.RWMutex

	// Consumer state
	handlers map[string]HandlerFunc // routing_key/queue -> handler
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Reconnection state
	reconnecting bool
	closeChan    chan *amqp.Error
}

// NewConnector creates a new RabbitMQ connector.
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

// Type returns "mq".
func (c *Connector) Type() string {
	return "mq"
}

// Connect establishes a connection to RabbitMQ.
func (c *Connector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return nil // Already connected
	}

	return c.connect()
}

// connect establishes the AMQP connection (must be called with lock held).
func (c *Connector) connect() error {
	var err error
	var conn *amqp.Connection

	// Build TLS config if needed
	var tlsConfig *amqp.Config
	if c.config.TLS != nil && c.config.TLS.Enabled {
		tls, err := c.config.TLS.BuildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		tlsConfig = &amqp.Config{
			TLSClientConfig: tls,
			Heartbeat:       c.config.Heartbeat,
		}
	} else {
		tlsConfig = &amqp.Config{
			Heartbeat: c.config.Heartbeat,
		}
	}

	if c.config.ConnectionName != "" {
		tlsConfig.Properties = amqp.Table{
			"connection_name": c.config.ConnectionName,
		}
	}

	// Connect
	conn, err = amqp.DialConfig(c.config.AMQPURL(), *tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create channel
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	c.conn = conn
	c.channel = channel

	// Set up close notification
	c.closeChan = make(chan *amqp.Error, 1)
	c.conn.NotifyClose(c.closeChan)

	c.logger.Info("connected to RabbitMQ",
		"name", c.name,
		"host", c.config.Host,
		"port", c.config.Port,
		"vhost", c.config.Vhost,
	)

	return nil
}

// Close closes the connection to RabbitMQ.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Signal shutdown to consumers
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for consumers to finish
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.logger.Warn("timeout waiting for consumers to finish")
	}

	// Close channel
	if c.channel != nil && !c.channel.IsClosed() {
		if err := c.channel.Close(); err != nil {
			c.logger.Debug("error closing channel", "error", err)
		}
	}

	// Close connection
	if c.conn != nil && !c.conn.IsClosed() {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
	}

	c.running = false
	c.logger.Info("disconnected from RabbitMQ", "name", c.name)
	return nil
}

// Health checks if the connection is alive.
func (c *Connector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil || c.conn.IsClosed() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	if c.channel == nil || c.channel.IsClosed() {
		return fmt.Errorf("channel is closed")
	}

	return nil
}

// Read implements connector.Reader for pull-based consumption.
// This is a one-shot read that pulls a single message from the queue.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.channel == nil || c.channel.IsClosed() {
		return nil, fmt.Errorf("channel is not available")
	}

	// Determine queue name
	queueName := query.Target
	if queueName == "" && c.config.Queue != nil {
		queueName = c.config.Queue.Name
	}
	if queueName == "" {
		return nil, fmt.Errorf("queue name is required")
	}

	// Pull a message
	autoAck := false
	if c.config.Consumer != nil {
		autoAck = c.config.Consumer.AutoAck
	}

	delivery, ok, err := c.channel.Get(queueName, autoAck)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	if !ok {
		// No message available
		return &connector.Result{
			Rows: []map[string]interface{}{},
		}, nil
	}

	// Parse message body
	var body map[string]interface{}
	if err := json.Unmarshal(delivery.Body, &body); err != nil {
		// If not JSON, wrap raw body
		body = map[string]interface{}{
			"raw": string(delivery.Body),
		}
	}

	// Acknowledge if not auto-ack
	if !autoAck {
		if err := delivery.Ack(false); err != nil {
			c.logger.Error("failed to acknowledge message", "error", err)
		}
	}

	return &connector.Result{
		Rows: []map[string]interface{}{body},
		Metadata: map[string]interface{}{
			"delivery_tag": delivery.DeliveryTag,
			"routing_key":  delivery.RoutingKey,
			"exchange":     delivery.Exchange,
			"redelivered":  delivery.Redelivered,
		},
	}, nil
}

// Write implements connector.Writer for publishing messages.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Create message from payload
	msg := types.NewMessage(data.Payload)

	// Set routing info
	if data.Target != "" {
		msg.RoutingKey = data.Target
	} else if c.config.Publisher != nil {
		msg.RoutingKey = c.config.Publisher.RoutingKey
	}

	if c.config.Publisher != nil {
		msg.Exchange = c.config.Publisher.Exchange
	}

	// Check for custom exchange/routing in params
	if exchange, ok := data.Params["exchange"].(string); ok {
		msg.Exchange = exchange
	}
	if routingKey, ok := data.Params["routing_key"].(string); ok {
		msg.RoutingKey = routingKey
	}

	// Publish
	if err := c.Publish(ctx, msg); err != nil {
		return nil, err
	}

	return &connector.Result{
		Affected: 1,
		Metadata: map[string]interface{}{
			"message_id":  msg.ID,
			"exchange":    msg.Exchange,
			"routing_key": msg.RoutingKey,
		},
	}, nil
}

// RegisterRoute registers a handler for a routing key pattern.
// This implements the RouteRegistrar interface for flow integration.
func (c *Connector) RegisterRoute(operation string, handler HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[operation] = handler
	c.logger.Debug("registered handler", "pattern", operation)
}

// Start begins consuming messages (implements Starter interface).
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("connector already running")
	}
	c.running = true
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	// Start consumer if configured
	if c.config.IsConsumer() {
		return c.startConsumer(c.ctx)
	}

	// For publishers, just start connection monitoring
	go c.monitorConnection()

	return nil
}

// monitorConnection watches for connection errors and reconnects.
func (c *Connector) monitorConnection() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case err := <-c.closeChan:
			if err != nil {
				c.logger.Error("connection closed", "error", err)
				c.handleReconnect()
			}
		}
	}
}

// handleReconnect attempts to reconnect to RabbitMQ.
func (c *Connector) handleReconnect() {
	c.mu.Lock()
	if c.reconnecting || !c.running {
		c.mu.Unlock()
		return
	}
	c.reconnecting = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.reconnecting = false
		c.mu.Unlock()
	}()

	for i := 0; i < c.config.MaxReconnects; i++ {
		c.logger.Info("attempting reconnection",
			"attempt", i+1,
			"max", c.config.MaxReconnects,
		)

		select {
		case <-c.ctx.Done():
			return
		case <-time.After(c.config.ReconnectDelay):
		}

		c.mu.Lock()
		err := c.connect()
		c.mu.Unlock()

		if err == nil {
			c.logger.Info("reconnected to RabbitMQ")

			// Restart consumer if needed
			if c.config.IsConsumer() {
				go func() {
					if err := c.startConsumer(c.ctx); err != nil {
						c.logger.Error("failed to restart consumer", "error", err)
					}
				}()
			}
			return
		}

		c.logger.Error("reconnection failed", "error", err)
	}

	c.logger.Error("max reconnection attempts reached")
}

// setupTopology declares exchanges and queues.
func (c *Connector) setupTopology() error {
	// Declare exchange if configured
	if c.config.Exchange != nil && c.config.Exchange.Name != "" {
		err := c.channel.ExchangeDeclare(
			c.config.Exchange.Name,
			string(c.config.Exchange.Type),
			c.config.Exchange.Durable,
			c.config.Exchange.AutoDelete,
			c.config.Exchange.Internal,
			c.config.Exchange.NoWait,
			c.config.Exchange.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to declare exchange: %w", err)
		}
		c.logger.Debug("declared exchange",
			"name", c.config.Exchange.Name,
			"type", c.config.Exchange.Type,
		)
	}

	// Declare queue if configured
	if c.config.Queue != nil && c.config.Queue.Name != "" {
		_, err := c.channel.QueueDeclare(
			c.config.Queue.Name,
			c.config.Queue.Durable,
			c.config.Queue.AutoDelete,
			c.config.Queue.Exclusive,
			c.config.Queue.NoWait,
			c.config.Queue.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to declare queue: %w", err)
		}
		c.logger.Debug("declared queue", "name", c.config.Queue.Name)

		// Bind queue to exchange if both are configured
		if c.config.Exchange != nil && c.config.Exchange.Name != "" {
			routingKey := c.config.Exchange.RoutingKey
			if routingKey == "" {
				routingKey = c.config.Queue.Name
			}

			err := c.channel.QueueBind(
				c.config.Queue.Name,
				routingKey,
				c.config.Exchange.Name,
				false,
				c.config.Exchange.BindArgs,
			)
			if err != nil {
				return fmt.Errorf("failed to bind queue: %w", err)
			}
			c.logger.Debug("bound queue to exchange",
				"queue", c.config.Queue.Name,
				"exchange", c.config.Exchange.Name,
				"routing_key", routingKey,
			)
		}
	}

	return nil
}

// QueueName returns the configured queue name.
func (c *Connector) QueueName() string {
	if c.config.Queue != nil {
		return c.config.Queue.Name
	}
	return ""
}

// ExchangeName returns the configured exchange name.
func (c *Connector) ExchangeName() string {
	if c.config.Exchange != nil {
		return c.config.Exchange.Name
	}
	if c.config.Publisher != nil {
		return c.config.Publisher.Exchange
	}
	return ""
}

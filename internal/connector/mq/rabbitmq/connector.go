package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/mq/types"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/tracing"
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

	// Filter rejection tracking for requeue dedup
	requeueTracker *flow.RequeueTracker

	// Debug throttling: studio-controlled single-message processing
	debugGate connector.DebugGate
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

	attrs := []any{
		"name", c.name,
		"host", c.config.Host,
		"port", c.config.Port,
		"vhost", c.config.Vhost,
	}
	if c.config.Queue != nil && c.config.Queue.Name != "" {
		attrs = append(attrs, "queue", c.config.Queue.Name)
	}
	c.logger.Info("connected to RabbitMQ", attrs...)

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

	// Stop requeue tracker
	if c.requeueTracker != nil {
		c.requeueTracker.Stop()
		c.requeueTracker = nil
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

	// Propagate the active distributed trace into the AMQP message headers so a
	// downstream consumer continues the same trace (no-op when tracing is off).
	msg.Headers = tracing.InjectInto(ctx, msg.Headers)

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
// Uses raw function type (not HandlerFunc alias) to satisfy Go interface matching.
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.handlers[operation]; ok {
		c.handlers[operation] = HandlerFunc(connector.ChainEventDriven(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "pattern", operation)
	} else {
		c.handlers[operation] = handler
	}
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
	c.requeueTracker = flow.NewRequeueTracker(10 * time.Minute)
	c.mu.Unlock()

	// Always watch the connection for drops. Both publishers AND consumers
	// must reconnect after an idle/network disconnect; consumers additionally
	// re-issue basic.consume via handleReconnect's IsConsumer() branch.
	//
	// This MUST run before the consumer branch below: that branch returns early,
	// so a consumer used to never start its monitor goroutine. The result was a
	// non-consuming zombie — after a single idle-disconnect the broker closed the
	// connection, the delivery channel closed (worker exited), the close error
	// landed on an unread closeChan, and the consumer never re-subscribed until
	// a full process restart. The handleReconnect re-subscribe logic existed but
	// was dead code because nothing watched the consumer's closeChan.
	go c.monitorConnection()

	// Start consumer if configured
	if c.config.IsConsumer() {
		return c.startConsumer(c.ctx)
	}

	return nil
}

// AllowOne permits exactly one message through the debug gate.
// Called when the IDE sends debug.consume.
func (c *Connector) AllowOne() {
	c.logger.Info("AllowOne called, gate enabled", "gate_enabled", c.debugGate.IsEnabled())
	c.debugGate.Allow()
	c.logger.Info("AllowOne: token placed in gate")
}

// SourceInfo returns the connector type and queue name for IDE display.
func (c *Connector) SourceInfo() (string, string) {
	queueName := ""
	if c.config.Queue != nil {
		queueName = c.config.Queue.Name
	}
	return "rabbitmq", queueName
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
//
// Both exchange and queue use a passive-first strategy so that shared,
// pre-existing infrastructure (typically owned by ops or another service)
// is preserved untouched. When the resource does not exist:
//   - create_if_missing=true → declare it actively
//   - create_if_missing=false (default since v2.0.0) → fail at startup with
//     a clear error, so typos and missing infra surface at deploy time
//     instead of leaving a consumer hanging silently on an empty auto-created
//     queue
//
// When the consumer queue exists, the retry-N-then-drop path still works via
// republish in handleRetry; Reject(false) at the final attempt discards the
// message instead of routing to a DLQ unless the queue's existing topology
// carries x-dead-letter-exchange.
func (c *Connector) setupTopology() error {
	// Declare exchange if configured
	if c.config.Exchange != nil && c.config.Exchange.Name != "" {
		if err := c.declareExchange(); err != nil {
			return err
		}
	}

	dlqConfig := c.getDLQConfig()

	// Declare queue if configured. DLX infrastructure (setupDLQ) is set up
	// only when we actually own the queue declaration — declaring orphan DLX
	// exchanges and DLQ queues for a pre-existing queue we did not configure
	// would create dead infrastructure that nothing routes to.
	if c.config.Queue != nil && c.config.Queue.Name != "" {
		queueExisted, err := c.declareConsumerQueue(dlqConfig)
		if err != nil {
			return fmt.Errorf("failed to declare queue: %w", err)
		}

		if !queueExisted && dlqConfig != nil && dlqConfig.Enabled {
			if err := c.setupDLQ(dlqConfig); err != nil {
				return fmt.Errorf("failed to setup DLQ: %w", err)
			}
		}

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

// declareConsumerQueue declares the consumer queue using a passive-first
// strategy. Returns true if the queue already existed (and was not redeclared
// by Mycel). Callers should skip DLX infrastructure setup in that case.
//
// AMQP semantics:
//   - QueueDeclarePassive succeeds → queue exists; do not redeclare so that
//     args mismatch (e.g. dlq{} adds x-dead-letter-exchange while the existing
//     queue has none) cannot produce 406 PRECONDITION_FAILED.
//   - QueueDeclarePassive fails with NotFound (404) → queue does not exist;
//     RabbitMQ has closed the channel server-side. If CreateIfMissing is true
//     we reopen the channel and active-declare with full args; if false (the
//     v2.0.0 default) we return a clear actionable error so the typo or
//     missing infra surfaces at deploy time.
//   - Any other passive error is treated as fatal and bubbled up.
func (c *Connector) declareConsumerQueue(dlqConfig *DLQConfig) (bool, error) {
	queueCfg := c.config.Queue

	_, passiveErr := c.channel.QueueDeclarePassive(
		queueCfg.Name,
		queueCfg.Durable,
		queueCfg.AutoDelete,
		queueCfg.Exclusive,
		queueCfg.NoWait,
		nil,
	)
	if passiveErr == nil {
		c.logger.Info("queue exists; preserving existing topology",
			"queue", queueCfg.Name,
		)
		if dlqConfig != nil && dlqConfig.Enabled {
			c.logger.Warn(
				"dlq enabled but queue pre-existed without Mycel-declared DLX args; "+
					"retry counting via republish still works, but on Reject(false) "+
					"RabbitMQ will discard messages instead of routing to a DLQ unless "+
					"a server-side policy with dead-letter-exchange is configured on this queue",
				"queue", queueCfg.Name,
				"max_retries", dlqConfig.MaxRetries,
			)
		}
		return true, nil
	}

	// Distinguish "queue not found" (expected) from real errors.
	if amqpErr, ok := passiveErr.(*amqp.Error); ok && amqpErr.Code != amqp.NotFound {
		return false, fmt.Errorf("failed to inspect queue: %w", passiveErr)
	}

	if !queueCfg.CreateIfMissing {
		return false, fmt.Errorf(
			"queue %q does not exist on broker %s (vhost %q). "+
				"Declare it externally (Terraform, rabbitmqctl, RabbitMQ Management UI) or, "+
				"for ephemeral environments, set create_if_missing = true on the consumer or queue block",
			queueCfg.Name, c.config.Host, c.config.Vhost,
		)
	}

	// Passive declare on a missing queue closes the channel server-side; reopen.
	if c.conn == nil || c.conn.IsClosed() {
		return false, fmt.Errorf("connection closed during topology setup: %w", passiveErr)
	}
	newChannel, chErr := c.conn.Channel()
	if chErr != nil {
		return false, fmt.Errorf("failed to reopen channel after passive declare: %w", chErr)
	}
	c.channel = newChannel

	args := queueCfg.Args
	if dlqConfig != nil && dlqConfig.Enabled {
		if args == nil {
			args = make(map[string]interface{})
		}
		dlxExchange := c.getDLXExchangeName(dlqConfig)
		args["x-dead-letter-exchange"] = dlxExchange
		if dlqConfig.RoutingKey != "" {
			args["x-dead-letter-routing-key"] = dlqConfig.RoutingKey
		}
	}

	_, err := c.channel.QueueDeclare(
		queueCfg.Name,
		queueCfg.Durable,
		queueCfg.AutoDelete,
		queueCfg.Exclusive,
		queueCfg.NoWait,
		args,
	)
	if err != nil {
		return false, err
	}

	c.logger.Debug("declared queue", "name", queueCfg.Name)
	return false, nil
}

// declareExchange declares the configured exchange using the same
// passive-first strategy as declareConsumerQueue: respect existing topology
// when the exchange exists; fail at startup when it does not unless
// create_if_missing is set.
func (c *Connector) declareExchange() error {
	exCfg := c.config.Exchange

	passiveErr := c.channel.ExchangeDeclarePassive(
		exCfg.Name,
		string(exCfg.Type),
		exCfg.Durable,
		exCfg.AutoDelete,
		exCfg.Internal,
		exCfg.NoWait,
		nil,
	)
	if passiveErr == nil {
		c.logger.Info("exchange exists; preserving existing topology",
			"name", exCfg.Name,
			"type", exCfg.Type,
		)
		return nil
	}

	if amqpErr, ok := passiveErr.(*amqp.Error); ok && amqpErr.Code != amqp.NotFound {
		return fmt.Errorf("failed to inspect exchange: %w", passiveErr)
	}

	if !exCfg.CreateIfMissing {
		return fmt.Errorf(
			"exchange %q does not exist on broker %s (vhost %q). "+
				"Declare it externally or, for ephemeral environments, set create_if_missing = true on the exchange block",
			exCfg.Name, c.config.Host, c.config.Vhost,
		)
	}

	// Passive declare on a missing exchange closes the channel server-side; reopen.
	if c.conn == nil || c.conn.IsClosed() {
		return fmt.Errorf("connection closed during topology setup: %w", passiveErr)
	}
	newChannel, chErr := c.conn.Channel()
	if chErr != nil {
		return fmt.Errorf("failed to reopen channel after passive declare: %w", chErr)
	}
	c.channel = newChannel

	if err := c.channel.ExchangeDeclare(
		exCfg.Name,
		string(exCfg.Type),
		exCfg.Durable,
		exCfg.AutoDelete,
		exCfg.Internal,
		exCfg.NoWait,
		exCfg.Args,
	); err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}
	c.logger.Debug("declared exchange",
		"name", exCfg.Name,
		"type", exCfg.Type,
	)
	return nil
}

// getDLQConfig returns the DLQ configuration if available.
func (c *Connector) getDLQConfig() *DLQConfig {
	if c.config.Consumer != nil && c.config.Consumer.DLQ != nil {
		return c.config.Consumer.DLQ
	}
	return nil
}

// getDLXExchangeName returns the dead letter exchange name.
func (c *Connector) getDLXExchangeName(dlqConfig *DLQConfig) string {
	if dlqConfig.Exchange != "" {
		return dlqConfig.Exchange
	}
	if c.config.Exchange != nil && c.config.Exchange.Name != "" {
		return c.config.Exchange.Name + ".dlx"
	}
	return "dlx"
}

// getDLQQueueName returns the dead letter queue name.
func (c *Connector) getDLQQueueName(dlqConfig *DLQConfig) string {
	if dlqConfig.Queue != "" {
		return dlqConfig.Queue
	}
	if c.config.Queue != nil && c.config.Queue.Name != "" {
		return c.config.Queue.Name + ".dlq"
	}
	return "dlq"
}

// setupDLQ sets up the dead letter exchange and queue.
func (c *Connector) setupDLQ(dlqConfig *DLQConfig) error {
	dlxExchange := c.getDLXExchangeName(dlqConfig)
	dlqQueue := c.getDLQQueueName(dlqConfig)

	// Declare DLX exchange
	err := c.channel.ExchangeDeclare(
		dlxExchange,
		"direct",
		true,  // durable
		false, // auto-delete
		false, // internal
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare DLX exchange: %w", err)
	}
	c.logger.Debug("declared DLX exchange", "name", dlxExchange)

	// Declare DLQ queue
	_, err = c.channel.QueueDeclare(
		dlqQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare DLQ queue: %w", err)
	}
	c.logger.Debug("declared DLQ queue", "name", dlqQueue)

	// Bind DLQ to DLX
	routingKey := dlqConfig.RoutingKey
	if routingKey == "" {
		routingKey = "#" // Catch all
	}

	err = c.channel.QueueBind(
		dlqQueue,
		routingKey,
		dlxExchange,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to bind DLQ queue: %w", err)
	}
	c.logger.Debug("bound DLQ queue to DLX",
		"queue", dlqQueue,
		"exchange", dlxExchange,
		"routing_key", routingKey,
	)

	return nil
}

// SetDebugMode enables or disables single-message debug throttling.
// When enabled, only one message is processed at a time, and the AMQP
// prefetch is set to 1 so the broker doesn't push extra messages.
func (c *Connector) SetDebugMode(enabled bool) {
	c.debugGate.SetEnabled(enabled)

	c.mu.RLock()
	ch := c.channel
	cfg := c.config.Consumer
	c.mu.RUnlock()

	if ch == nil || ch.IsClosed() {
		return
	}

	if enabled {
		ch.Qos(1, 0, false)
		c.logger.Info("debug mode enabled: prefetch=1, single-message processing", "name", c.name)
	} else {
		// Restore original prefetch
		prefetch := 10
		if cfg != nil && cfg.Prefetch > 0 {
			prefetch = cfg.Prefetch
		}
		ch.Qos(prefetch, 0, false)
		c.logger.Info("debug mode disabled: restored prefetch", "name", c.name, "prefetch", prefetch)
	}
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

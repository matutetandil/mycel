package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/redis/go-redis/v9"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/mq/types"
)

// HandlerFunc is the function signature for message handlers.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Connector is a Redis Pub/Sub connector that supports both subscribing and publishing.
type Connector struct {
	name   string
	config *Config
	logger *slog.Logger

	// Redis client
	client *redis.Client

	// Pub/Sub subscription
	pubsub *redis.PubSub

	// Handler registration
	handlers map[string]HandlerFunc // channel or pattern -> handler
	mu       sync.RWMutex

	// Lifecycle
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewConnector creates a new Redis Pub/Sub connector.
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
		logger:   logger.With("connector", name, "type", "redis-pubsub"),
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

// Connect establishes a connection to Redis and verifies it with PING.
func (c *Connector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return nil // Already connected
	}

	c.client = redis.NewClient(&redis.Options{
		Addr:     c.config.Addr(),
		Password: c.config.Password,
		DB:       c.config.DB,
	})

	// Verify connectivity
	if err := c.client.Ping(ctx).Err(); err != nil {
		c.client.Close()
		c.client = nil
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	c.logger.Info("connected to Redis",
		"name", c.name,
		"addr", c.config.Addr(),
		"db", c.config.DB,
	)

	return nil
}

// Close unsubscribes, closes pub/sub and client connections.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Signal shutdown to subscription goroutines
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for goroutines to finish
	c.wg.Wait()

	// Close pub/sub
	if c.pubsub != nil {
		if err := c.pubsub.Close(); err != nil {
			c.logger.Debug("error closing pubsub", "error", err)
		}
		c.pubsub = nil
	}

	// Close client
	if c.client != nil {
		if err := c.client.Close(); err != nil {
			return fmt.Errorf("failed to close Redis client: %w", err)
		}
		c.client = nil
	}

	c.running = false
	c.logger.Info("disconnected from Redis", "name", c.name)
	return nil
}

// Health checks if the Redis connection is alive via PING.
func (c *Connector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis health check failed: %w", err)
	}

	return nil
}

// RegisterRoute registers a handler for a channel name or pattern.
// The operation is the channel name that the handler will receive messages for.
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[operation] = handler
	c.logger.Debug("registered handler", "channel", operation)
}

// Start begins subscribing to configured channels and patterns, dispatching
// incoming messages to registered handlers.
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("connector already running")
	}
	c.running = true
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	// If no channels/patterns configured, nothing to subscribe to
	if !c.config.IsSubscriber() {
		return nil
	}

	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	// Build subscriptions
	var channels []string
	var patterns []string

	channels = append(channels, c.config.Channels...)
	patterns = append(patterns, c.config.Patterns...)

	// Create PubSub and subscribe
	pubsub := client.Subscribe(c.ctx)

	if len(channels) > 0 {
		if err := pubsub.Subscribe(c.ctx, channels...); err != nil {
			pubsub.Close()
			return fmt.Errorf("failed to subscribe to channels: %w", err)
		}
	}

	if len(patterns) > 0 {
		if err := pubsub.PSubscribe(c.ctx, patterns...); err != nil {
			pubsub.Close()
			return fmt.Errorf("failed to psubscribe to patterns: %w", err)
		}
	}

	c.mu.Lock()
	c.pubsub = pubsub
	c.mu.Unlock()

	c.logger.Info("started Redis Pub/Sub subscriber",
		"channels", channels,
		"patterns", patterns,
	)

	// Start message receive loop
	c.wg.Add(1)
	go c.receiveLoop()

	return nil
}

// receiveLoop reads messages from the pub/sub subscription and dispatches them.
func (c *Connector) receiveLoop() {
	defer c.wg.Done()

	ch := c.pubsub.Channel()

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Debug("subscription loop stopping")
			return
		case msg, ok := <-ch:
			if !ok {
				c.logger.Debug("pubsub channel closed")
				return
			}
			c.handleMessage(msg)
		}
	}
}

// handleMessage processes a single Redis Pub/Sub message.
func (c *Connector) handleMessage(msg *redis.Message) {
	// Build input map with metadata
	input := ParseMessage(msg)

	// Find handler: try exact channel match first, then pattern match, then wildcard
	c.mu.RLock()
	handler := c.findHandler(msg.Channel, msg.Pattern)
	c.mu.RUnlock()

	if handler == nil {
		c.logger.Warn("no handler for channel",
			"channel", msg.Channel,
			"pattern", msg.Pattern,
		)
		return
	}

	// Execute handler
	if _, err := handler(c.ctx, input); err != nil {
		c.logger.Error("handler error",
			"channel", msg.Channel,
			"error", err,
		)
	}
}

// findHandler locates the appropriate handler for a message.
// Priority: exact channel match > pattern match > wildcard "*".
func (c *Connector) findHandler(channel, pattern string) HandlerFunc {
	// Try exact channel match
	if handler, ok := c.handlers[channel]; ok {
		return handler
	}

	// Try pattern match (for psubscribe messages)
	if pattern != "" {
		if handler, ok := c.handlers[pattern]; ok {
			return handler
		}
	}

	// Try wildcard handler
	if handler, ok := c.handlers["*"]; ok {
		return handler
	}

	return nil
}

// Write publishes a message to a Redis Pub/Sub channel.
// data.Target is the channel name, data.Payload is the message content (JSON-serialized).
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	// Create message from payload
	msg := types.NewMessage(data.Payload)

	// Determine channel from target
	channel := data.Target
	if channel == "" {
		// Try to get from params
		if ch, ok := data.Params["channel"].(string); ok {
			channel = ch
		}
	}
	if channel == "" {
		return nil, fmt.Errorf("channel is required for publishing (set target or params.channel)")
	}

	// Serialize the message body
	body, err := json.Marshal(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize message body: %w", err)
	}

	// PUBLISH
	receivers, err := client.Publish(ctx, channel, body).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to publish message to channel %s: %w", channel, err)
	}

	c.logger.Debug("published message",
		"id", msg.ID,
		"channel", channel,
		"receivers", receivers,
	)

	return &connector.Result{
		Affected: 1,
		Metadata: map[string]interface{}{
			"message_id": msg.ID,
			"channel":    channel,
			"receivers":  receivers,
		},
	}, nil
}

// Publish publishes a types.Message to the specified channel (via RoutingKey).
func (c *Connector) Publish(ctx context.Context, msg *types.Message) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	channel := msg.RoutingKey
	if channel == "" {
		return fmt.Errorf("channel (routing_key) is required for publishing")
	}

	body, err := json.Marshal(msg.Body)
	if err != nil {
		return fmt.Errorf("failed to serialize message body: %w", err)
	}

	if err := client.Publish(ctx, channel, body).Err(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	c.logger.Debug("published message",
		"id", msg.ID,
		"channel", channel,
	)

	return nil
}

// ParseMessage converts a Redis Pub/Sub message into the standard input map format.
// The resulting map contains _channel, _pattern (if present), and the parsed JSON
// payload fields merged at the top level.
func ParseMessage(msg *redis.Message) map[string]interface{} {
	input := map[string]interface{}{
		"_channel": msg.Channel,
	}

	if msg.Pattern != "" {
		input["_pattern"] = msg.Pattern
	}

	// Try to parse payload as JSON and merge fields
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Payload), &parsed); err == nil {
		for k, v := range parsed {
			input[k] = v
		}
	} else {
		// If not JSON, store raw payload
		input["raw"] = msg.Payload
	}

	return input
}

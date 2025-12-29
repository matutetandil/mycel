package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// startConsumer starts consuming messages from the configured queue.
func (c *Connector) startConsumer(ctx context.Context) error {
	// Set up topology (exchanges, queues, bindings)
	if err := c.setupTopology(); err != nil {
		return fmt.Errorf("failed to setup topology: %w", err)
	}

	// Determine queue name
	queueName := ""
	if c.config.Queue != nil {
		queueName = c.config.Queue.Name
	}
	if queueName == "" {
		return fmt.Errorf("queue name is required for consumer")
	}

	// Get consumer config
	consumerCfg := c.config.Consumer
	if consumerCfg == nil {
		consumerCfg = DefaultConsumerConfig()
	}

	// Set QoS (prefetch)
	if consumerCfg.Prefetch > 0 {
		err := c.channel.Qos(consumerCfg.Prefetch, 0, false)
		if err != nil {
			return fmt.Errorf("failed to set QoS: %w", err)
		}
	}

	// Start consuming
	deliveries, err := c.channel.Consume(
		queueName,
		consumerCfg.Tag,
		consumerCfg.AutoAck,
		consumerCfg.Exclusive,
		consumerCfg.NoLocal,
		consumerCfg.NoWait,
		consumerCfg.Args,
	)
	if err != nil {
		return fmt.Errorf("failed to start consumer: %w", err)
	}

	c.logger.Info("started consuming",
		"name", c.name,
		"queue", queueName,
		"concurrency", consumerCfg.Concurrency,
		"prefetch", consumerCfg.Prefetch,
	)

	// Start worker goroutines
	concurrency := consumerCfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	for i := 0; i < concurrency; i++ {
		c.wg.Add(1)
		go c.consumeWorker(ctx, deliveries, i)
	}

	return nil
}

// consumeWorker processes messages from the delivery channel.
func (c *Connector) consumeWorker(ctx context.Context, deliveries <-chan amqp.Delivery, workerID int) {
	defer c.wg.Done()

	c.logger.Debug("consumer worker started", "worker_id", workerID)

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("consumer worker stopping", "worker_id", workerID)
			return

		case delivery, ok := <-deliveries:
			if !ok {
				c.logger.Debug("delivery channel closed", "worker_id", workerID)
				return
			}

			if err := c.handleDelivery(ctx, delivery); err != nil {
				c.logger.Error("failed to handle delivery",
					"worker_id", workerID,
					"error", err,
					"routing_key", delivery.RoutingKey,
				)
			}
		}
	}
}

// handleDelivery processes a single message delivery.
func (c *Connector) handleDelivery(ctx context.Context, delivery amqp.Delivery) error {
	// Parse message body
	var body map[string]interface{}
	if err := json.Unmarshal(delivery.Body, &body); err != nil {
		// If not JSON, wrap raw body
		body = map[string]interface{}{
			"raw": string(delivery.Body),
		}
	}

	// Find handler for this routing key
	c.mu.RLock()
	handler := c.findHandler(delivery.RoutingKey)
	c.mu.RUnlock()

	if handler == nil {
		c.logger.Warn("no handler for routing key",
			"routing_key", delivery.RoutingKey,
			"exchange", delivery.Exchange,
		)
		// Nack without requeue for unhandled messages
		return delivery.Nack(false, false)
	}

	// Execute handler
	_, err := handler(ctx, body)
	if err != nil {
		c.logger.Error("handler error",
			"routing_key", delivery.RoutingKey,
			"error", err,
		)
		// Nack with requeue on error
		return delivery.Nack(false, true)
	}

	// Acknowledge successful processing
	if c.config.Consumer != nil && !c.config.Consumer.AutoAck {
		return delivery.Ack(false)
	}

	return nil
}

// findHandler finds a handler for the given routing key.
// It supports exact match and wildcard patterns (for topic exchanges).
func (c *Connector) findHandler(routingKey string) HandlerFunc {
	// Try exact match first
	if handler, ok := c.handlers[routingKey]; ok {
		return handler
	}

	// Try pattern matching for topic exchanges
	for pattern, handler := range c.handlers {
		if matchRoutingKey(pattern, routingKey) {
			return handler
		}
	}

	// Try wildcard handler (catches all)
	if handler, ok := c.handlers["*"]; ok {
		return handler
	}
	if handler, ok := c.handlers["#"]; ok {
		return handler
	}

	return nil
}

// matchRoutingKey checks if a routing key matches a pattern.
// Supports AMQP topic exchange patterns:
// - * matches exactly one word
// - # matches zero or more words
func matchRoutingKey(pattern, routingKey string) bool {
	if pattern == routingKey {
		return true
	}
	if pattern == "#" || pattern == "*" {
		return true
	}

	// Simple pattern matching for topic exchanges
	patternParts := splitRoutingKey(pattern)
	keyParts := splitRoutingKey(routingKey)

	return matchParts(patternParts, keyParts)
}

// splitRoutingKey splits a routing key by dots.
func splitRoutingKey(key string) []string {
	if key == "" {
		return []string{}
	}

	parts := []string{}
	current := ""
	for _, c := range key {
		if c == '.' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// matchParts matches pattern parts against key parts.
func matchParts(pattern, key []string) bool {
	pi, ki := 0, 0

	for pi < len(pattern) {
		if pi >= len(pattern) {
			return ki >= len(key)
		}

		switch pattern[pi] {
		case "#":
			// # matches zero or more words
			if pi == len(pattern)-1 {
				return true // # at end matches everything
			}
			// Try matching remaining pattern at each position
			for ki <= len(key) {
				if matchParts(pattern[pi+1:], key[ki:]) {
					return true
				}
				ki++
			}
			return false

		case "*":
			// * matches exactly one word
			if ki >= len(key) {
				return false
			}
			pi++
			ki++

		default:
			// Exact match required
			if ki >= len(key) || pattern[pi] != key[ki] {
				return false
			}
			pi++
			ki++
		}
	}

	return ki >= len(key)
}

// Ack acknowledges a message by delivery tag.
func (c *Connector) Ack(deliveryTag uint64, multiple bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.channel == nil || c.channel.IsClosed() {
		return fmt.Errorf("channel is not available")
	}

	return c.channel.Ack(deliveryTag, multiple)
}

// Nack negatively acknowledges a message.
func (c *Connector) Nack(deliveryTag uint64, multiple, requeue bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.channel == nil || c.channel.IsClosed() {
		return fmt.Errorf("channel is not available")
	}

	return c.channel.Nack(deliveryTag, multiple, requeue)
}

// Reject rejects a message.
func (c *Connector) Reject(deliveryTag uint64, requeue bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.channel == nil || c.channel.IsClosed() {
		return fmt.Errorf("channel is not available")
	}

	return c.channel.Reject(deliveryTag, requeue)
}

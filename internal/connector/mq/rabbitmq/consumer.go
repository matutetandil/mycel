package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/internal/flow"
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

			c.logger.Info("worker received delivery, waiting for gate",
				"worker_id", workerID,
				"routing_key", delivery.RoutingKey,
				"size", len(delivery.Body),
			)
			c.debugGate.Acquire()
			c.logger.Info("worker passed gate, processing",
				"worker_id", workerID,
				"routing_key", delivery.RoutingKey,
			)
			err := c.handleDelivery(ctx, delivery)
			c.debugGate.Release()
			if err != nil {
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
	var body interface{}
	if err := json.Unmarshal(delivery.Body, &body); err != nil {
		// If not JSON, use raw string
		body = string(delivery.Body)
	}

	// Convert AMQP headers to map[string]interface{}
	headers := make(map[string]interface{})
	for k, v := range delivery.Headers {
		headers[k] = v
	}

	// Build AMQP properties map
	properties := map[string]interface{}{
		"message_id":       delivery.MessageId,
		"correlation_id":   delivery.CorrelationId,
		"content_type":     delivery.ContentType,
		"content_encoding": delivery.ContentEncoding,
		"delivery_mode":    delivery.DeliveryMode,
		"priority":         delivery.Priority,
		"reply_to":         delivery.ReplyTo,
		"expiration":       delivery.Expiration,
		"type":             delivery.Type,
		"user_id":          delivery.UserId,
		"app_id":           delivery.AppId,
		"timestamp":        delivery.Timestamp.Unix(),
		"delivery_tag":     delivery.DeliveryTag,
		"redelivered":      delivery.Redelivered,
	}

	// Build the full input structure for MQ messages
	// input.body - the parsed message payload
	// input.headers - AMQP headers
	// input.properties - AMQP message properties
	// input.routing_key - the routing key
	// input.exchange - the exchange name
	input := map[string]interface{}{
		"body":        body,
		"headers":     headers,
		"properties":  properties,
		"routing_key": delivery.RoutingKey,
		"exchange":    delivery.Exchange,
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

	// Execute handler with the full input structure
	result, err := handler(ctx, input)
	if err != nil {
		c.logger.Error("handler error",
			"routing_key", delivery.RoutingKey,
			"error", err,
		)
		// Handle retry logic
		return c.handleRetry(delivery, err)
	}

	// Check if the result is a filter rejection with policy
	if filtered, ok := result.(*flow.FilteredResultWithPolicy); ok && filtered.Filtered {
		return c.handleFilterReject(delivery, filtered)
	}

	// Acknowledge successful processing
	if c.config.Consumer != nil && !c.config.Consumer.AutoAck {
		return delivery.Ack(false)
	}

	return nil
}

// handleFilterReject handles a message that was rejected by a filter expression.
func (c *Connector) handleFilterReject(delivery amqp.Delivery, filtered *flow.FilteredResultWithPolicy) error {
	if c.config.Consumer != nil && c.config.Consumer.AutoAck {
		return nil // Already auto-acked
	}

	switch filtered.Policy {
	case "reject":
		// NACK without requeue — goes to DLX/DLQ if configured
		c.logger.Debug("filter reject: sending to DLQ",
			"routing_key", delivery.RoutingKey,
			"message_id", delivery.MessageId,
		)
		return delivery.Nack(false, false)

	case "requeue":
		// Check dedup tracker to prevent infinite requeue
		msgID := filtered.MessageID
		if msgID == "" {
			msgID = delivery.MessageId
		}
		if msgID == "" {
			// No message ID available, fall back to ACK
			c.logger.Warn("filter requeue: no message ID available, ACKing instead",
				"routing_key", delivery.RoutingKey,
			)
			return delivery.Ack(false)
		}

		maxRequeue := filtered.MaxRequeue
		if maxRequeue <= 0 {
			maxRequeue = 3
		}

		count, shouldAck := c.requeueTracker.IncrementAndCheck(msgID, maxRequeue)
		if shouldAck {
			c.logger.Debug("filter requeue: max attempts reached, ACKing",
				"routing_key", delivery.RoutingKey,
				"message_id", msgID,
				"attempts", count,
			)
			return delivery.Ack(false)
		}

		c.logger.Debug("filter requeue: returning to queue",
			"routing_key", delivery.RoutingKey,
			"message_id", msgID,
			"attempt", count,
			"max", maxRequeue,
		)
		return delivery.Nack(false, true)

	default: // "ack" or unknown
		return delivery.Ack(false)
	}
}

// handleRetry handles retry logic for failed messages.
func (c *Connector) handleRetry(delivery amqp.Delivery, handlerErr error) error {
	dlqConfig := c.getDLQConfig()
	if dlqConfig == nil || !dlqConfig.Enabled {
		// DLQ not enabled, just requeue
		return delivery.Nack(false, true)
	}

	// Get retry count from headers
	retryHeader := dlqConfig.RetryHeader
	if retryHeader == "" {
		retryHeader = "x-retry-count"
	}

	retryCount := 0
	if val, ok := delivery.Headers[retryHeader]; ok {
		switch v := val.(type) {
		case int32:
			retryCount = int(v)
		case int64:
			retryCount = int(v)
		case int:
			retryCount = v
		}
	}

	maxRetries := dlqConfig.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // Default
	}

	if retryCount >= maxRetries {
		// Max retries exceeded, send to DLQ
		c.logger.Warn("max retries exceeded, sending to DLQ",
			"routing_key", delivery.RoutingKey,
			"retry_count", retryCount,
			"max_retries", maxRetries,
		)
		// Reject without requeue - RabbitMQ will route to DLX/DLQ
		return delivery.Reject(false)
	}

	// Increment retry count and republish
	retryCount++
	c.logger.Debug("retrying message",
		"routing_key", delivery.RoutingKey,
		"retry_count", retryCount,
		"max_retries", maxRetries,
	)

	// Build new headers with updated retry count
	newHeaders := make(amqp.Table)
	for k, v := range delivery.Headers {
		newHeaders[k] = v
	}
	newHeaders[retryHeader] = int32(retryCount)
	newHeaders["x-last-error"] = handlerErr.Error()

	// Determine exchange and routing key for retry
	exchange := delivery.Exchange
	routingKey := delivery.RoutingKey

	// Publish retry message
	err := c.channel.PublishWithContext(
		context.Background(),
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Headers:         newHeaders,
			ContentType:     delivery.ContentType,
			ContentEncoding: delivery.ContentEncoding,
			DeliveryMode:    delivery.DeliveryMode,
			Priority:        delivery.Priority,
			CorrelationId:   delivery.CorrelationId,
			ReplyTo:         delivery.ReplyTo,
			Expiration:      delivery.Expiration,
			MessageId:       delivery.MessageId,
			Timestamp:       delivery.Timestamp,
			Type:            delivery.Type,
			UserId:          delivery.UserId,
			AppId:           delivery.AppId,
			Body:            delivery.Body,
		},
	)
	if err != nil {
		c.logger.Error("failed to publish retry message",
			"error", err,
			"routing_key", routingKey,
		)
		// If we can't republish, reject to DLQ
		return delivery.Reject(false)
	}

	// Acknowledge the original message (it's been republished)
	return delivery.Ack(false)
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

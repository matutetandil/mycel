package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/internal/connector/mq/types"
)

// Publish publishes a message to RabbitMQ.
func (c *Connector) Publish(ctx context.Context, msg *types.Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.channel == nil || c.channel.IsClosed() {
		return fmt.Errorf("channel is not available")
	}

	publishing, err := c.buildPublishing(msg)
	if err != nil {
		return err
	}

	// Determine exchange
	exchange := msg.Exchange
	if exchange == "" && c.config.Publisher != nil {
		exchange = c.config.Publisher.Exchange
	}

	// Determine routing key
	routingKey := msg.RoutingKey
	if routingKey == "" && c.config.Publisher != nil {
		routingKey = c.config.Publisher.RoutingKey
	}

	// Get publish options
	var mandatory, immediate bool
	if c.config.Publisher != nil {
		mandatory = c.config.Publisher.Mandatory
		immediate = c.config.Publisher.Immediate
	}

	// Publish with context
	err = c.channel.PublishWithContext(
		ctx,
		exchange,
		routingKey,
		mandatory,
		immediate,
		publishing,
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	c.logger.Debug("published message",
		"id", msg.ID,
		"exchange", exchange,
		"routing_key", routingKey,
	)

	return nil
}

// PublishWithConfirm publishes a message and waits for broker confirmation.
func (c *Connector) PublishWithConfirm(ctx context.Context, msg *types.Message) error {
	c.mu.Lock()

	if c.channel == nil || c.channel.IsClosed() {
		c.mu.Unlock()
		return fmt.Errorf("channel is not available")
	}

	// Enable confirm mode
	if err := c.channel.Confirm(false); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to enable confirm mode: %w", err)
	}

	confirms := c.channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	c.mu.Unlock()

	// Publish the message
	if err := c.Publish(ctx, msg); err != nil {
		return err
	}

	// Wait for confirmation
	select {
	case <-ctx.Done():
		return ctx.Err()
	case confirm := <-confirms:
		if !confirm.Ack {
			return fmt.Errorf("message was not acknowledged by broker")
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for confirmation")
	}

	return nil
}

// buildPublishing creates an AMQP Publishing from a Message.
func (c *Connector) buildPublishing(msg *types.Message) (amqp.Publishing, error) {
	// Serialize body
	body, err := json.Marshal(msg.Body)
	if err != nil {
		return amqp.Publishing{}, fmt.Errorf("failed to serialize message body: %w", err)
	}

	// Determine content type
	contentType := msg.ContentType
	if contentType == "" && c.config.Publisher != nil {
		contentType = c.config.Publisher.ContentType
	}
	if contentType == "" {
		contentType = "application/json"
	}

	// Determine delivery mode (persistent or transient)
	var deliveryMode uint8 = uint8(types.DeliveryModeTransient)
	if c.config.Publisher != nil && c.config.Publisher.Persistent {
		deliveryMode = uint8(types.DeliveryModePersistent)
	}

	// Build AMQP headers
	var headers amqp.Table
	if len(msg.Headers) > 0 {
		headers = make(amqp.Table)
		for k, v := range msg.Headers {
			headers[k] = v
		}
	}

	// Build timestamp
	var timestamp time.Time
	if msg.Timestamp > 0 {
		timestamp = time.Unix(msg.Timestamp, 0)
	} else {
		timestamp = time.Now()
	}

	return amqp.Publishing{
		Headers:      headers,
		ContentType:  contentType,
		DeliveryMode: deliveryMode,
		MessageId:    msg.ID,
		Timestamp:    timestamp,
		Body:         body,
	}, nil
}

// PublishBatch publishes multiple messages in a batch.
func (c *Connector) PublishBatch(ctx context.Context, messages []*types.Message) error {
	for _, msg := range messages {
		if err := c.Publish(ctx, msg); err != nil {
			return fmt.Errorf("failed to publish message %s: %w", msg.ID, err)
		}
	}
	return nil
}

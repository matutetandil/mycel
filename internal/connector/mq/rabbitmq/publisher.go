package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
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

	// Without confirms, a publish is a write to a socket: it returns as soon
	// as the bytes are handed over, and a broker that dies a moment later
	// takes the message with it.
	if c.config.Publisher == nil || !c.config.Publisher.Confirms {
		if err := c.channel.PublishWithContext(
			ctx, exchange, routingKey, mandatory, immediate, publishing,
		); err != nil {
			return fmt.Errorf("failed to publish message: %w", err)
		}

		c.logger.Debug("published message",
			"id", msg.ID,
			"exchange", exchange,
			"routing_key", routingKey,
		)
		return nil
	}

	// With confirms, the publish does not return until the broker says it has
	// the message — which is the difference between a flow that acknowledges
	// its source after publishing downstream and one that acknowledges after
	// writing to a socket.
	//
	// The channel was put into confirm mode when it was opened; this waits
	// for the confirmation belonging to this publish, so concurrent publishes
	// do not read each other's.
	confirmation, err := c.channel.PublishWithDeferredConfirmWithContext(
		ctx, exchange, routingKey, mandatory, immediate, publishing,
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	acked, err := c.awaitConfirmation(ctx, confirmation)
	if err != nil {
		return err
	}
	if !acked {
		// A nack means the broker refused it — most often the exchange is
		// there and nothing is bound to route it to, or the queue is full and
		// overflow is set to reject.
		return fmt.Errorf("the broker did not accept the message published to %q with routing key %q",
			exchange, routingKey)
	}

	c.logger.Debug("published message, confirmed by the broker",
		"id", msg.ID,
		"exchange", exchange,
		"routing_key", routingKey,
	)

	return nil
}

// confirmTimeout bounds the wait for a broker that never answers.
//
// The caller's context wins when it has a deadline of its own; this is for the
// ones that do not, so a broker that has stopped confirming blocks a flow for
// half a minute rather than for ever.
const confirmTimeout = 30 * time.Second

// awaitConfirmation waits for the broker's answer about one publish.
func (c *Connector) awaitConfirmation(ctx context.Context, confirmation *amqp.DeferredConfirmation) (bool, error) {
	if confirmation == nil {
		// The library returns nil when the channel is not in confirm mode.
		// Treated as unconfirmed rather than as a refusal: the message did go
		// out, and calling it a failure would have the flow send it twice.
		c.logger.Warn("publisher confirms are configured and the channel is not in confirm mode",
			"connector", c.name)
		return true, nil
	}

	wait := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		wait, cancel = context.WithTimeout(ctx, confirmTimeout)
		defer cancel()
	}

	select {
	case <-wait.Done():
		return false, fmt.Errorf("gave up waiting for the broker to confirm the message: %w", wait.Err())
	case <-confirmation.Done():
		return confirmation.Acked(), nil
	}
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

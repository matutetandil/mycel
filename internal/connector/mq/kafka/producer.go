package kafka

import (
	"context"
	"fmt"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/matutetandil/mycel/internal/connector/mq/types"
)

// Publish publishes a message to Kafka.
func (c *Connector) Publish(ctx context.Context, msg *types.Message) error {
	c.mu.RLock()
	writer := c.writer
	c.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("producer not initialized")
	}

	kafkaMsg, err := c.buildKafkaMessage(msg)
	if err != nil {
		return err
	}

	// If no topic in message, use default from config
	if kafkaMsg.Topic == "" && c.config.Producer != nil {
		kafkaMsg.Topic = c.config.Producer.Topic
	}

	if kafkaMsg.Topic == "" {
		return fmt.Errorf("topic is required")
	}

	if err := writer.WriteMessages(ctx, kafkaMsg); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	c.logger.Debug("published message",
		"id", msg.ID,
		"topic", kafkaMsg.Topic,
	)

	return nil
}

// PublishBatch publishes multiple messages in a batch.
func (c *Connector) PublishBatch(ctx context.Context, messages []*types.Message) error {
	c.mu.RLock()
	writer := c.writer
	c.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("producer not initialized")
	}

	kafkaMessages := make([]kafkago.Message, 0, len(messages))
	for _, msg := range messages {
		kafkaMsg, err := c.buildKafkaMessage(msg)
		if err != nil {
			return fmt.Errorf("failed to build message %s: %w", msg.ID, err)
		}

		// If no topic in message, use default from config
		if kafkaMsg.Topic == "" && c.config.Producer != nil {
			kafkaMsg.Topic = c.config.Producer.Topic
		}

		if kafkaMsg.Topic == "" {
			return fmt.Errorf("topic is required for message %s", msg.ID)
		}

		kafkaMessages = append(kafkaMessages, kafkaMsg)
	}

	if err := writer.WriteMessages(ctx, kafkaMessages...); err != nil {
		return fmt.Errorf("failed to publish batch: %w", err)
	}

	c.logger.Debug("published batch",
		"count", len(messages),
	)

	return nil
}

// PublishToTopic publishes a message to a specific topic.
func (c *Connector) PublishToTopic(ctx context.Context, topic string, msg *types.Message) error {
	msg.RoutingKey = topic
	return c.Publish(ctx, msg)
}

// PublishWithKey publishes a message with a specific partition key.
func (c *Connector) PublishWithKey(ctx context.Context, key string, msg *types.Message) error {
	c.mu.RLock()
	writer := c.writer
	c.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("producer not initialized")
	}

	kafkaMsg, err := c.buildKafkaMessage(msg)
	if err != nil {
		return err
	}

	// Override key for partitioning
	kafkaMsg.Key = []byte(key)

	// If no topic in message, use default from config
	if kafkaMsg.Topic == "" && c.config.Producer != nil {
		kafkaMsg.Topic = c.config.Producer.Topic
	}

	if kafkaMsg.Topic == "" {
		return fmt.Errorf("topic is required")
	}

	if err := writer.WriteMessages(ctx, kafkaMsg); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

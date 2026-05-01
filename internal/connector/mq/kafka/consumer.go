package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
)

// startConsumer starts consuming messages from the configured topics.
func (c *Connector) startConsumer(ctx context.Context) error {
	consumerCfg := c.config.Consumer
	if consumerCfg == nil {
		return fmt.Errorf("consumer configuration is required")
	}

	if len(consumerCfg.Topics) == 0 {
		return fmt.Errorf("at least one topic is required")
	}

	if consumerCfg.GroupID == "" {
		return fmt.Errorf("consumer group_id is required")
	}

	// Map start offset
	var startOffset int64
	switch consumerCfg.AutoOffsetReset {
	case "earliest":
		startOffset = kafka.FirstOffset
	case "latest":
		startOffset = kafka.LastOffset
	default:
		startOffset = kafka.FirstOffset
	}

	// Create reader configuration
	readerConfig := kafka.ReaderConfig{
		Brokers:        c.config.Brokers,
		GroupID:        consumerCfg.GroupID,
		GroupTopics:    consumerCfg.Topics,
		MinBytes:       consumerCfg.MinBytes,
		MaxBytes:       consumerCfg.MaxBytes,
		MaxWait:        consumerCfg.MaxWaitTime,
		StartOffset:    startOffset,
		CommitInterval: time.Second, // Commit offsets every second if auto-commit is enabled
	}

	// Handle TLS if configured
	if c.config.TLS != nil && c.config.TLS.Enabled {
		dialer := &kafka.Dialer{
			Timeout:   10 * time.Second,
			DualStack: true,
		}

		tlsConfig, err := c.config.TLS.BuildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		dialer.TLS = tlsConfig

		// Handle SASL if configured
		if c.config.SASL != nil {
			mechanism, err := c.buildSASLMechanism()
			if err != nil {
				return fmt.Errorf("failed to build SASL mechanism: %w", err)
			}
			dialer.SASLMechanism = mechanism
		}

		readerConfig.Dialer = dialer
	}

	// Only log actual errors from the kafka-go library (skip debug-level noise)
	readerConfig.ErrorLogger = kafka.LoggerFunc(func(msg string, args ...interface{}) {
		c.logger.Warn(fmt.Sprintf("kafka-reader: "+msg, args...))
	})

	c.reader = kafka.NewReader(readerConfig)

	c.logger.Info("started consumer",
		"group_id", consumerCfg.GroupID,
		"topics", consumerCfg.Topics,
		"concurrency", consumerCfg.Concurrency,
	)

	// Start consumer workers
	concurrency := consumerCfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	c.running = true
	for i := 0; i < concurrency; i++ {
		c.wg.Add(1)
		go c.consumeLoop(ctx, i)
	}

	return nil
}

// consumeLoop reads messages from Kafka and processes them.
func (c *Connector) consumeLoop(ctx context.Context, workerID int) {
	defer c.wg.Done()

	c.logger.Debug("consumer worker started", "worker_id", workerID)

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("consumer worker stopping", "worker_id", workerID)
			return
		default:
			// Read message with context
			msg, err := c.reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				c.logger.Error("failed to read message",
					"worker_id", workerID,
					"error", err,
				)
				continue
			}

			c.debugGate.Acquire()
			err = c.handleMessage(ctx, msg)
			c.debugGate.Release()
			if err != nil {
				c.logger.Error("failed to handle message",
					"worker_id", workerID,
					"topic", msg.Topic,
					"partition", msg.Partition,
					"offset", msg.Offset,
					"error", err,
				)
			}
		}
	}
}

// handleMessage processes a single Kafka message.
func (c *Connector) handleMessage(ctx context.Context, msg kafka.Message) error {
	// Parse message body
	var body interface{}
	if err := json.Unmarshal(msg.Value, &body); err != nil {
		// If not JSON, use raw string
		body = string(msg.Value)
	}

	// Convert Kafka headers to map[string]interface{}
	headers := make(map[string]interface{})
	for _, h := range msg.Headers {
		headers[h.Key] = string(h.Value)
	}

	// Build the full input structure for Kafka messages
	// input.body - the parsed message payload
	// input.headers - Kafka headers
	// input.topic - the topic name
	// input.partition - the partition number
	// input.offset - the message offset
	// input.key - the message key
	// input.timestamp - the message timestamp
	input := map[string]interface{}{
		"body":      body,
		"headers":   headers,
		"topic":     msg.Topic,
		"partition": msg.Partition,
		"offset":    msg.Offset,
		"key":       string(msg.Key),
		"timestamp": msg.Time.Unix(),
	}

	// Find handler for this topic
	c.mu.RLock()
	handler := c.findHandler(msg.Topic)
	c.mu.RUnlock()

	if handler == nil {
		c.logger.Warn("no handler for topic",
			"topic", msg.Topic,
		)
		return nil // Don't error, just skip
	}

	// Execute handler with the full input structure
	result, err := handler(ctx, input)
	if err != nil {
		c.logger.Error("handler error",
			"topic", msg.Topic,
			"error", err,
		)
		// Permanent failures (HTTP 4xx etc.) cannot be fixed by replaying.
		// Return nil so the offset commits and the message is not
		// re-consumed (Kafka offset semantics — equivalent to ack on
		// AMQP). Without this branch a 4xx blocks consumer-group
		// progress on the partition.
		if connector.IsPermanent(err) {
			c.logger.Warn("permanent flow failure, committing offset to skip",
				"topic", msg.Topic,
				"partition", msg.Partition,
				"offset", msg.Offset,
				"action", "commit",
				"reason", "permanent_failure",
				"error", err,
			)
			return nil
		}
		return err
	}

	// Fire any deferred on_drop closure attached to the result. The
	// flow handler defers firing so fan-out aggregation can suppress
	// siblings whose filter rejected when another sibling passed its
	// filter. No-op on success or when no on_drop aspects registered.
	flow.FireDropAspect(ctx, result)

	// Check if the result is a filter rejection with policy
	if filtered, ok := result.(*flow.FilteredResultWithPolicy); ok && filtered.Filtered {
		return c.handleFilterReject(ctx, msg, filtered)
	}

	return nil
}

// handleFilterReject handles a message that was rejected by a filter expression.
func (c *Connector) handleFilterReject(ctx context.Context, msg kafka.Message, filtered *flow.FilteredResultWithPolicy) error {
	switch filtered.Policy {
	case "reject":
		// Republish to <topic>.dlq
		dlqTopic := msg.Topic + ".dlq"
		c.logger.Info("filter reject (→ DLQ topic)",
			"topic", msg.Topic,
			"dlq_topic", dlqTopic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"action", "republish_dlq",
		)
		return c.republishMessage(ctx, dlqTopic, msg)

	case "requeue":
		// Republish to same topic with dedup tracking
		msgID := filtered.MessageID
		if msgID == "" {
			// Try to get from message key
			msgID = string(msg.Key)
		}
		if msgID == "" {
			// No message ID available, skip silently
			c.logger.Warn("filter requeue: no message ID available, skipping",
				"topic", msg.Topic,
				"partition", msg.Partition,
				"offset", msg.Offset,
				"action", "skip",
			)
			return nil
		}

		maxRequeue := filtered.MaxRequeue
		if maxRequeue <= 0 {
			maxRequeue = 3
		}

		count, shouldAck := c.requeueTracker.IncrementAndCheck(msgID, maxRequeue)
		if shouldAck {
			c.logger.Info("filter requeue exhausted, skipping",
				"topic", msg.Topic,
				"partition", msg.Partition,
				"offset", msg.Offset,
				"message_id", msgID,
				"action", "skip",
				"attempts", count,
				"max", maxRequeue,
			)
			return nil // Skip silently (offset already committed)
		}

		c.logger.Info("filter requeue",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"message_id", msgID,
			"action", "republish_same_topic",
			"attempt", count,
			"max", maxRequeue,
		)
		return c.republishMessage(ctx, msg.Topic, msg)

	default: // "ack" or unknown
		return nil // No-op, offset auto-committed
	}
}

// republishMessage republishes a Kafka message to a target topic.
func (c *Connector) republishMessage(ctx context.Context, topic string, msg kafka.Message) error {
	writer, err := c.ensureWriter()
	if err != nil {
		return err
	}

	newMsg := kafka.Message{
		Topic:   topic,
		Key:     msg.Key,
		Value:   msg.Value,
		Headers: msg.Headers,
	}

	return writer.WriteMessages(ctx, newMsg)
}

// ensureWriter lazily initializes a Kafka writer for reject/requeue operations
// when the connector is consumer-only.
func (c *Connector) ensureWriter() (*kafka.Writer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writer != nil {
		return c.writer, nil
	}

	c.writer = &kafka.Writer{
		Addr:     kafka.TCP(c.config.Brokers...),
		Balancer: &kafka.LeastBytes{},
	}

	// Configure Transport for TLS/SASL if needed
	if c.config.TLS != nil || c.config.SASL != nil {
		transport := &kafka.Transport{}

		if c.config.TLS != nil && c.config.TLS.Enabled {
			tlsConfig, err := c.config.TLS.BuildTLSConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to build TLS config for writer: %w", err)
			}
			transport.TLS = tlsConfig
		}

		if c.config.SASL != nil {
			mechanism, err := c.buildSASLMechanism()
			if err != nil {
				return nil, fmt.Errorf("failed to build SASL mechanism for writer: %w", err)
			}
			transport.SASL = mechanism
		}

		c.writer.Transport = transport
	}

	return c.writer, nil
}

// buildSASLMechanism creates a SASL mechanism from config.
func (c *Connector) buildSASLMechanism() (sasl.Mechanism, error) {
	if c.config.SASL == nil {
		return nil, nil
	}

	switch c.config.SASL.Mechanism {
	case "PLAIN":
		return plain.Mechanism{
			Username: c.config.SASL.Username,
			Password: c.config.SASL.Password,
		}, nil
	case "SCRAM-SHA-256":
		mechanism, err := scram.Mechanism(scram.SHA256, c.config.SASL.Username, c.config.SASL.Password)
		if err != nil {
			return nil, err
		}
		return mechanism, nil
	case "SCRAM-SHA-512":
		mechanism, err := scram.Mechanism(scram.SHA512, c.config.SASL.Username, c.config.SASL.Password)
		if err != nil {
			return nil, err
		}
		return mechanism, nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", c.config.SASL.Mechanism)
	}
}

// CommitMessages manually commits message offsets.
func (c *Connector) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	if c.reader == nil {
		return fmt.Errorf("consumer not initialized")
	}
	return c.reader.CommitMessages(ctx, msgs...)
}

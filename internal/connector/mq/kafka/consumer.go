package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/mq/undispatched"
	"github.com/matutetandil/mycel/v3/internal/flow"
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

	// Create reader configuration.
	//
	// A commit interval above zero is what makes the reader commit offsets on
	// a timer, which is auto-commit; zero means offsets move only when this
	// connector commits them. The interval used to be set unconditionally
	// while the comment beside it said "if auto-commit is enabled" — so
	// `auto_commit = false`, which is what somebody sets precisely so a
	// message is not lost when the flow handling it fails, did nothing at all.
	readerConfig := kafka.ReaderConfig{
		Brokers:     c.config.Brokers,
		GroupID:     consumerCfg.GroupID,
		GroupTopics: consumerCfg.Topics,
		MinBytes:    consumerCfg.MinBytes,
		MaxBytes:    consumerCfg.MaxBytes,
		MaxWait:     consumerCfg.MaxWaitTime,
		StartOffset: startOffset,
	}
	if consumerCfg.AutoCommit {
		readerConfig.CommitInterval = time.Second
	}

	// The connections this reader makes — to a broker, and to the coordinator
	// of its group — carry whatever the connector was told to present.
	//
	// SASL used to be attached only inside the TLS branch, so a consumer
	// configured with credentials and no TLS never presented them: a
	// SASL_PLAINTEXT listener, which is how an internal broker is usually
	// reached, answered EOF to the group coordinator lookup and the consumer
	// sat there logging "Unable to establish connection to consumer group
	// coordinator" and reading nothing, for as long as the service ran. The
	// producer beside it had it right, so the same configuration published
	// happily and consumed nothing.
	if c.config.TLS != nil || c.config.SASL != nil {
		dialer := &kafka.Dialer{
			Timeout:   10 * time.Second,
			DualStack: true,
		}

		if c.config.TLS != nil && c.config.TLS.Enabled {
			tlsConfig, err := c.config.TLS.BuildTLSConfig()
			if err != nil {
				return fmt.Errorf("failed to build TLS config: %w", err)
			}
			dialer.TLS = tlsConfig
		}

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

	c.mu.RLock()
	handlerCount := len(c.handlers)
	c.mu.RUnlock()

	c.logger.Info("started consumer",
		"group_id", consumerCfg.GroupID,
		"topics", consumerCfg.Topics,
		"concurrency", consumerCfg.Concurrency,
	)

	if handlerCount == 0 {
		undispatched.ReportNoHandlers(c.logger, c.name, "kafka", strings.Join(consumerCfg.Topics, ","))
	}

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

	// The reader is taken once, for the life of the loop.
	//
	// Close cancels this context, waits a bounded time for the workers, and
	// then releases the reader — so a worker still in flight when that bound
	// runs out would read c.reader as nil and dereference it. That is a
	// segmentation fault on the way out: a service consuming from Kafka
	// crashed on every shutdown instead of exiting, which in Kubernetes is a
	// crash report on every rolling update, and locally it was losing the
	// coverage counters a -cover binary writes as it exits.
	c.mu.Lock()
	reader := c.reader
	c.mu.Unlock()

	if reader == nil {
		c.logger.Debug("consumer worker has no reader, stopping", "worker_id", workerID)
		return
	}

	c.logger.Debug("consumer worker started", "worker_id", workerID)

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("consumer worker stopping", "worker_id", workerID)
			return
		default:
			// ReadMessage moves the offset by itself; FetchMessage leaves it
			// where it was until this connector commits, which is the only
			// way a failed message can be seen again.
			msg, err := c.nextMessage(ctx, reader)
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
				// Left uncommitted on purpose: a restart resumes from the
				// last committed offset and this message is read again.
				// Kafka has no way to return one message on its own, so this
				// is what "do not lose it" means here.
				continue
			}

			if !c.autoCommitting() {
				if err := reader.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
					c.logger.Error("failed to commit offset after handling the message",
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
}

// autoCommitting reports whether offsets move on their own.
func (c *Connector) autoCommitting() bool {
	return c.config.Consumer == nil || c.config.Consumer.AutoCommit
}

// nextMessage reads the next message, in the way the commit setting requires.
func (c *Connector) nextMessage(ctx context.Context, reader *kafka.Reader) (kafka.Message, error) {
	if c.autoCommitting() {
		return reader.ReadMessage(ctx)
	}
	return reader.FetchMessage(ctx)
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
	patterns := undispatched.SortedPatterns(c.handlers)
	c.mu.RUnlock()

	if handler == nil {
		// Returning nil lets the caller commit the offset, so the message is
		// gone for good — worth stating, since it differs from RabbitMQ where
		// a dead-letter exchange could still catch it.
		c.undispatched.Report(c.logger, undispatched.Event{
			Connector:   c.name,
			Driver:      "kafka",
			Target:      msg.Topic,
			Key:         msg.Topic,
			Patterns:    patterns,
			Consequence: "offset committed; the message will not be redelivered",
		})
		return nil // Don't error, just skip
	}

	// Execute handler with the full input structure
	result, err := handler(ctx, input)
	if err != nil {
		c.logger.Error("handler error",
			"topic", msg.Topic,
			"error", err,
		)
		// Explicit disposition chosen by the flow's error_handling
		// (on_timeout / on_error). Takes precedence over the IsPermanent
		// inference below. Kafka has no per-message nack, so dispositions map
		// onto offset semantics: ack/reject commit the offset (skip), and
		// reject additionally republishes to <topic>.dlq; requeue returns the
		// error so the offset is not committed and the message is re-consumed.
		if disp, ok := connector.GetDisposition(err); ok {
			switch disp {
			case connector.DispositionAck:
				c.logger.Warn("flow disposition: ack (commit/skip)",
					"topic", msg.Topic,
					"partition", msg.Partition,
					"offset", msg.Offset,
					"action", "commit",
					"error", err,
				)
				return nil
			case connector.DispositionReject:
				dlqTopic := msg.Topic + ".dlq"
				c.logger.Warn("flow disposition: reject (→ DLQ topic)",
					"topic", msg.Topic,
					"dlq_topic", dlqTopic,
					"partition", msg.Partition,
					"offset", msg.Offset,
					"action", "republish_dlq",
					"error", err,
				)
				return c.republishMessage(ctx, dlqTopic, msg)
			case connector.DispositionRequeue:
				return err
			}
		}
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

		// Wait for the replicas. This writer is the one a consumer builds to
		// republish — to the dead-letter topic, or back onto its own for a
		// bounded retry — and it had no setting at all, which is fire and
		// forget: the offset for the original message commits, so a republish
		// the broker drops leaves the message in neither place. The
		// dead-letter topic exists so that nothing is lost.
		RequiredAcks: kafka.RequireAll,
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

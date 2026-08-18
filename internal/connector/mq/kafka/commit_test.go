package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// Whether an offset moves before the flow has done its work.
//
// Kafka has no way to return one message on its own: the only thing that
// decides whether a message is seen again is whether its offset was committed.
// `auto_commit = false` is what somebody sets so a message whose flow failed
// is not skipped, and it was read from the configuration into a field nothing
// used — offsets committed on a timer either way, so a failed message was
// passed over and never came back.

func consumerOn(t *testing.T, topic, group string, autoCommit bool, handler func(context.Context, map[string]interface{}) (interface{}, error)) *Connector {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Brokers = kafkaTestBrokers(t)
	cfg.Producer = nil
	cfg.Consumer = &ConsumerConfig{
		GroupID:     group,
		Topics:      []string{topic},
		AutoCommit:  autoCommit,
		MinBytes:    1,
		MaxBytes:    10e6,
		MaxWaitTime: 200 * time.Millisecond,
		Concurrency: 1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c, err := NewConnector(t.Name(), cfg, logger)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	c.RegisterRoute(topic, handler)
	return c
}

// createTopic makes the topic before anything uses it.
//
// The broker creates topics on demand, but the first publish to one that does
// not exist yet is refused while it is being made — which reads as a test
// failure rather than as the broker catching up.
func createTopic(t *testing.T, topic string) {
	t.Helper()

	client := &kafkago.Client{Addr: kafkago.TCP(kafkaTestBrokers(t)...)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := client.CreateTopics(ctx, &kafkago.CreateTopicsRequest{
		Topics: []kafkago.TopicConfig{{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}},
	}); err != nil {
		t.Fatalf("creating topic %s: %v", topic, err)
	}

	// Wait for it to be visible: creation is asynchronous across the cluster.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		metadata, err := client.Metadata(ctx, &kafkago.MetadataRequest{Topics: []string{topic}})
		if err == nil {
			for _, found := range metadata.Topics {
				if found.Name == topic && found.Error == nil && len(found.Partitions) > 0 {
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("topic %s never became available", topic)
}

// publish puts one message on the topic, without going through the connector.
func publish(t *testing.T, topic, body string) {
	t.Helper()

	writer := &kafkago.Writer{
		Addr:         kafkago.TCP(kafkaTestBrokers(t)...),
		Topic:        topic,
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafkago.RequireAll,
	}
	defer writer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := writer.WriteMessages(ctx, kafkago.Message{Value: []byte(body)}); err != nil {
		t.Fatalf("publishing the test message: %v", err)
	}
}

// committedOffset asks the broker where the group is up to.
func committedOffset(t *testing.T, topic, group string) int64 {
	t.Helper()

	client := &kafkago.Client{Addr: kafkago.TCP(kafkaTestBrokers(t)...)}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	answer, err := client.OffsetFetch(ctx, &kafkago.OffsetFetchRequest{
		GroupID: group,
		Topics:  map[string][]int{topic: {0}},
	})
	if err != nil {
		t.Fatalf("OffsetFetch: %v", err)
	}
	for _, partitions := range answer.Topics {
		for _, partition := range partitions {
			if partition.Partition == 0 {
				return partition.CommittedOffset
			}
		}
	}
	return -1
}

func TestAnOffsetStaysPutWhenTheFlowFails(t *testing.T) {
	// The whole point of auto_commit = false: the message is still there to be
	// read again, because nothing said it had been dealt with.
	topic := fmt.Sprintf("mycel_commit_%d", time.Now().UnixNano())
	group := topic + "_group"
	createTopic(t, topic)

	handled := make(chan struct{}, 8)
	c := consumerOn(t, topic, group, false, func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		handled <- struct{}{}
		return nil, errors.New("the supplier's API is down")
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	publish(t, topic, `{"order_id":"order-1"}`)
	waitForHandling(t, handled)

	// Give a background committer every chance to have run, if one were
	// mistakenly enabled.
	time.Sleep(2 * time.Second)
	cancel()
	_ = c.Close(context.Background())

	if offset := committedOffset(t, topic, group); offset > 0 {
		t.Errorf("the group is committed at offset %d after the flow failed, so the message will never be read again", offset)
	}
}

func TestAnOffsetMovesWhenTheFlowSucceeds(t *testing.T) {
	// The other half: work that was done is not done twice.
	topic := fmt.Sprintf("mycel_commit_ok_%d", time.Now().UnixNano())
	group := topic + "_group"
	createTopic(t, topic)

	handled := make(chan struct{}, 8)
	c := consumerOn(t, topic, group, false, func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		handled <- struct{}{}
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	publish(t, topic, `{"order_id":"order-1"}`)
	waitForHandling(t, handled)

	time.Sleep(2 * time.Second)
	cancel()
	_ = c.Close(context.Background())

	if offset := committedOffset(t, topic, group); offset < 1 {
		t.Errorf("the group is committed at offset %d after the flow succeeded, so the message is read again on every restart", offset)
	}
}

func waitForHandling(t *testing.T, handled chan struct{}) {
	t.Helper()
	select {
	case <-handled:
	case <-time.After(30 * time.Second):
		t.Fatal("the message never reached the flow")
	}
}

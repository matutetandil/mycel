package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// How this connector asks to be treated by a broker.
//
// Everything that decides whether a subscription survives the broker
// restarting — which is the failure mode this project audited every
// long-running source for — is set here, in one function that nothing
// exercised. MQTT came out of that audit as safe precisely because the
// library reconnects and this handler re-subscribes, and neither half was
// checked.

func connectorFor(t *testing.T, config *Config) *Connector {
	t.Helper()
	c, err := NewConnector("devices", config, slog.New(slog.NewTextHandler(discard{}, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	return c
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestWhatTheBrokerIsTold(t *testing.T) {
	c := connectorFor(t, &Config{
		Broker:               "tcp://broker.internal:1883",
		ClientID:             "orders-consumer-1",
		Username:             "mycel",
		Password:             "secret",
		QoS:                  1,
		CleanSession:         false,
		KeepAlive:            30 * time.Second,
		ConnectTimeout:       10 * time.Second,
		AutoReconnect:        true,
		MaxReconnectInterval: 2 * time.Minute,
	})

	opts := c.buildClientOptions()

	if len(opts.Servers) != 1 || opts.Servers[0].Host != "broker.internal:1883" {
		t.Errorf("broker = %v", opts.Servers)
	}
	// The client id is what a broker keys a session on: two consumers sharing
	// one disconnect each other in a loop.
	if opts.ClientID != "orders-consumer-1" {
		t.Errorf("client id = %q", opts.ClientID)
	}
	if opts.Username != "mycel" || opts.Password != "secret" {
		t.Errorf("credentials = %q/%q", opts.Username, opts.Password)
	}
	// A session that is not clean is what makes the broker hold messages for
	// a consumer that was away — the whole reason to set it.
	if opts.CleanSession {
		t.Error("clean_session was set to false and the broker was told true")
	}
	if opts.KeepAlive != 30 {
		t.Errorf("keep alive = %v seconds", opts.KeepAlive)
	}
	if opts.ConnectTimeout != 10*time.Second {
		t.Errorf("connect timeout = %v", opts.ConnectTimeout)
	}
	// Without these two the connector stops consuming the first time the
	// broker restarts, and nothing says so.
	if !opts.AutoReconnect {
		t.Error("auto reconnect is off, so a broker restart ends the subscription")
	}
	if opts.MaxReconnectInterval != 2*time.Minute {
		t.Errorf("max reconnect interval = %v", opts.MaxReconnectInterval)
	}
}

func TestReconnectingResubscribes(t *testing.T) {
	// The half that matters. The library reconnects on its own; the
	// subscriptions do not come back with it, so they are made again here.
	// Without this a consumer reconnects, reports itself connected, and
	// receives nothing ever again.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})

	var subscribed sync.Map
	fake := &fakeClient{connected: true, onSubscribe: func(topic string) {
		subscribed.Store(topic, true)
	}}

	c.RegisterRoute("orders/+/created", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	c.RegisterRoute("orders/+/cancelled", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	c.mu.Lock()
	c.client = fake
	c.running = true
	c.mu.Unlock()

	opts := c.buildClientOptions()
	if opts.OnConnect == nil {
		t.Fatal("nothing happens when the connection comes back")
	}
	opts.OnConnect(fake)

	for _, topic := range []string{"orders/+/created", "orders/+/cancelled"} {
		if _, ok := subscribed.Load(topic); !ok {
			t.Errorf("%s was not subscribed to again after reconnecting", topic)
		}
	}
}

func TestAConnectionThatComesBackBeforeTheConnectorHasStarted(t *testing.T) {
	// The library connects during start-up, before any route is registered.
	// Re-subscribing then would subscribe to nothing and log failures.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})

	var subscribes int
	fake := &fakeClient{connected: true, onSubscribe: func(string) { subscribes++ }}
	c.mu.Lock()
	c.client = fake
	c.running = false
	c.mu.Unlock()

	c.buildClientOptions().OnConnect(fake)

	if subscribes != 0 {
		t.Errorf("subscribed %d times before the connector was running", subscribes)
	}
}

func TestSubscribingWithNoConnection(t *testing.T) {
	// Said plainly. Paho queues a subscribe on a disconnected client and it
	// silently never takes effect.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})

	err := c.subscribe("orders/+/created", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("subscribed through a client that is not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("error = %v", err)
	}
}

func TestWhatAReceivedMessageBecomes(t *testing.T) {
	// What a flow reads. The topic is the useful part: a subscription with a
	// wildcard receives from many, and which one is only in here.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})

	received := make(chan map[string]interface{}, 1)
	handler := c.buildMessageHandler("orders/+/created",
		func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			received <- input
			return nil, nil
		})

	handler(nil, &fakeMessage{
		topic:     "orders/nz/created",
		payload:   []byte(`{"order_id":"order-1","total":42.5}`),
		qos:       1,
		messageID: 7,
		retained:  true,
	})

	select {
	case input := <-received:
		if input["_topic"] != "orders/nz/created" {
			t.Errorf("topic = %v, and a wildcard subscription has no other way to tell", input["_topic"])
		}
		if input["order_id"] != "order-1" {
			t.Errorf("the payload was not decoded: %v", input)
		}
		if input["_qos"] != byte(1) {
			t.Errorf("qos = %v", input["_qos"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the message never reached the flow")
	}
}

func TestAHandlerThatFailsDoesNotStopTheSubscription(t *testing.T) {
	// MQTT has nothing to reject a message with — no nack, no requeue — so a
	// failure is logged and the next message must still arrive. Letting the
	// panic or the error escape would take the paho router down with it.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})

	var handled int
	handler := c.buildMessageHandler("orders/+/created",
		func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			handled++
			return nil, context.DeadlineExceeded
		})

	message := &fakeMessage{topic: "orders/nz/created", payload: []byte(`{"order_id":"order-1"}`)}
	handler(nil, message)
	handler(nil, message)

	if handled != 2 {
		t.Errorf("the handler ran %d times; a failure stopped the next message", handled)
	}
}

// publishedMessage is what the fake client was asked to send.
type publishedMessage struct {
	topic    string
	qos      byte
	retained bool
	payload  []byte
}

var errTopicRefused = errors.New("not authorised to publish to that topic")

// fakeClient is a paho client that answers without a broker.
type fakeClient struct {
	pahomqtt.Client
	connected   bool
	onSubscribe func(topic string)
	onPublish   func(publishedMessage)
	publishErr  error
}

func (f *fakeClient) IsConnected() bool { return f.connected }
func (f *fakeClient) Publish(topic string, qos byte, retained bool, payload interface{}) pahomqtt.Token {
	if f.onPublish != nil {
		body, _ := payload.([]byte)
		f.onPublish(publishedMessage{topic: topic, qos: qos, retained: retained, payload: body})
	}
	return &doneToken{err: f.publishErr}
}
func (f *fakeClient) Subscribe(topic string, qos byte, callback pahomqtt.MessageHandler) pahomqtt.Token {
	if f.onSubscribe != nil {
		f.onSubscribe(topic)
	}
	return &doneToken{}
}

type doneToken struct{ err error }

func (doneToken) Wait() bool                     { return true }
func (doneToken) WaitTimeout(time.Duration) bool { return true }
func (doneToken) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
func (d doneToken) Error() error { return d.err }

// fakeMessage is a message as the broker would deliver it.
type fakeMessage struct {
	topic     string
	payload   []byte
	qos       byte
	messageID uint16
	retained  bool
}

func (m *fakeMessage) Duplicate() bool   { return false }
func (m *fakeMessage) Qos() byte         { return m.qos }
func (m *fakeMessage) Retained() bool    { return m.retained }
func (m *fakeMessage) Topic() string     { return m.topic }
func (m *fakeMessage) MessageID() uint16 { return m.messageID }
func (m *fakeMessage) Payload() []byte   { return m.payload }
func (m *fakeMessage) Ack()              {}

func TestPublishingToATopic(t *testing.T) {
	// A flow writing to MQTT. The topic and the retain flag are the two that
	// change what other people see: a retained message is delivered to
	// everyone who subscribes afterwards, for as long as it stands.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders", Topic: "orders/out", QoS: 1})

	published := make(chan publishedMessage, 4)
	c.mu.Lock()
	c.client = &fakeClient{connected: true, onPublish: func(m publishedMessage) { published <- m }}
	c.mu.Unlock()

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "orders/nz/created",
		Payload: map[string]interface{}{"order_id": "order-1"},
		Params:  map[string]interface{}{"qos": 2, "retain": true},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}

	sent := <-published
	if sent.topic != "orders/nz/created" {
		t.Errorf("topic = %q, want the flow's target", sent.topic)
	}
	if sent.qos != 2 {
		t.Errorf("qos = %d, want the one the flow asked for", sent.qos)
	}
	if !sent.retained {
		t.Error("retain was asked for and the message was not retained")
	}
	if !strings.Contains(string(sent.payload), "order-1") {
		t.Errorf("payload = %s", sent.payload)
	}

	// With no target, the connector's own topic is used.
	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-2"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if sent := <-published; sent.topic != "orders/out" {
		t.Errorf("topic = %q, want the connector's own", sent.topic)
	}
}

func TestPublishingWithNowhereToPublishTo(t *testing.T) {
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})
	c.mu.Lock()
	c.client = &fakeClient{connected: true}
	c.mu.Unlock()

	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-1"},
	}); err == nil {
		t.Error("a message was published to no topic at all")
	}
}

func TestPublishingBeforeThereIsAConnection(t *testing.T) {
	// Paho queues a publish on a disconnected client and it is lost quietly.
	// Failing the write lets the flow's error handling decide instead.
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders", Topic: "orders/out"})

	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-1"},
	}); err == nil {
		t.Fatal("a message was published through a client that is not connected")
	}

	c.mu.Lock()
	c.client = &fakeClient{connected: false}
	c.mu.Unlock()
	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-1"},
	}); err == nil {
		t.Error("a message was published through a disconnected client")
	}
}

func TestAPublishTheBrokerRefused(t *testing.T) {
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders", Topic: "orders/out"})
	c.mu.Lock()
	c.client = &fakeClient{connected: true, publishErr: errTopicRefused}
	c.mu.Unlock()

	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-1"},
	}); err == nil {
		t.Error("a publish the broker refused was reported as sent")
	}
}

func TestTheConnectorDescribesItselfToTheRuntime(t *testing.T) {
	c := connectorFor(t, &Config{Broker: "tcp://broker.internal:1883", ClientID: "orders"})

	if c.Name() != "devices" || c.Type() != "mqtt" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
	kind, name := c.SourceInfo()
	if kind != "mqtt" || name != "devices" {
		t.Errorf("source info = %s/%s", kind, name)
	}

	// The debug gate: with a debugger attached, one message at a time.
	c.SetDebugMode(true)
	c.AllowOne()
	c.SetDebugMode(false)
}

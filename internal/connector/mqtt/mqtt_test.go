package mqtt

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// mockMessage implements pahomqtt.Message for testing ParseMessage.
type mockMessage struct {
	topic     string
	messageID uint16
	qos       byte
	retained  bool
	payload   []byte
	duplicate bool
	ack       func()
}

func (m *mockMessage) Duplicate() bool  { return m.duplicate }
func (m *mockMessage) Qos() byte        { return m.qos }
func (m *mockMessage) Retained() bool   { return m.retained }
func (m *mockMessage) Topic() string    { return m.topic }
func (m *mockMessage) MessageID() uint16 { return m.messageID }
func (m *mockMessage) Payload() []byte  { return m.payload }
func (m *mockMessage) Ack()             { if m.ack != nil { m.ack() } }

func TestMQTTDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Broker != "tcp://localhost:1883" {
		t.Errorf("expected default broker tcp://localhost:1883, got %s", cfg.Broker)
	}
	if cfg.ClientID != "mycel" {
		t.Errorf("expected default client_id mycel, got %s", cfg.ClientID)
	}
	if cfg.QoS != 0 {
		t.Errorf("expected default QoS 0, got %d", cfg.QoS)
	}
	if !cfg.CleanSession {
		t.Error("expected default clean_session true")
	}
	if cfg.KeepAlive != 30*time.Second {
		t.Errorf("expected default keep_alive 30s, got %s", cfg.KeepAlive)
	}
	if !cfg.AutoReconnect {
		t.Error("expected default auto_reconnect true")
	}
}

func TestMQTTConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "empty broker",
			config:  &Config{ClientID: "test"},
			wantErr: true,
		},
		{
			name:    "empty client_id",
			config:  &Config{Broker: "tcp://localhost:1883"},
			wantErr: true,
		},
		{
			name:    "invalid QoS",
			config:  &Config{Broker: "tcp://localhost:1883", ClientID: "test", QoS: 3},
			wantErr: true,
		},
		{
			name:    "QoS 0 valid",
			config:  &Config{Broker: "tcp://localhost:1883", ClientID: "test", QoS: 0},
			wantErr: false,
		},
		{
			name:    "QoS 1 valid",
			config:  &Config{Broker: "tcp://localhost:1883", ClientID: "test", QoS: 1},
			wantErr: false,
		},
		{
			name:    "QoS 2 valid",
			config:  &Config{Broker: "tcp://localhost:1883", ClientID: "test", QoS: 2},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMQTTConnectorNameType(t *testing.T) {
	cfg := DefaultConfig()
	c, err := NewConnector("test_mqtt", cfg, nil)
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	if c.Name() != "test_mqtt" {
		t.Errorf("Name() = %s, want test_mqtt", c.Name())
	}
	if c.Type() != "mqtt" {
		t.Errorf("Type() = %s, want mqtt", c.Type())
	}
}

func TestMQTTRegisterRoute(t *testing.T) {
	cfg := DefaultConfig()
	c, err := NewConnector("test_mqtt", cfg, nil)
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	called := false
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		called = true
		return nil, nil
	}

	c.RegisterRoute("sensors/temperature", handler)
	c.RegisterRoute("sensors/+/data", handler)
	c.RegisterRoute("events/#", handler)

	handlers := c.TopicHandlers()
	if len(handlers) != 3 {
		t.Errorf("expected 3 handlers, got %d", len(handlers))
	}

	if _, ok := handlers["sensors/temperature"]; !ok {
		t.Error("handler for sensors/temperature not found")
	}
	if _, ok := handlers["sensors/+/data"]; !ok {
		t.Error("handler for sensors/+/data not found")
	}
	if _, ok := handlers["events/#"]; !ok {
		t.Error("handler for events/# not found")
	}

	// Verify handler is callable
	_, _ = handlers["sensors/temperature"](context.Background(), nil)
	if !called {
		t.Error("handler was not called")
	}
}

func TestMQTTMessageParsingJSON(t *testing.T) {
	payload := map[string]interface{}{
		"temperature": 23.5,
		"humidity":    60.0,
		"sensor":     "s1",
	}
	body, _ := json.Marshal(payload)

	msg := &mockMessage{
		topic:     "sensors/temperature",
		messageID: 42,
		qos:       1,
		retained:  false,
		payload:   body,
	}

	input := ParseMessage(msg)

	// Check metadata fields
	if input["_topic"] != "sensors/temperature" {
		t.Errorf("_topic = %v, want sensors/temperature", input["_topic"])
	}
	if input["_message_id"] != uint16(42) {
		t.Errorf("_message_id = %v, want 42", input["_message_id"])
	}
	if input["_qos"] != byte(1) {
		t.Errorf("_qos = %v, want 1", input["_qos"])
	}
	if input["_retained"] != false {
		t.Errorf("_retained = %v, want false", input["_retained"])
	}

	// Check parsed payload fields
	if temp, ok := input["temperature"].(float64); !ok || temp != 23.5 {
		t.Errorf("temperature = %v, want 23.5", input["temperature"])
	}
	if hum, ok := input["humidity"].(float64); !ok || hum != 60.0 {
		t.Errorf("humidity = %v, want 60.0", input["humidity"])
	}
	if sensor, ok := input["sensor"].(string); !ok || sensor != "s1" {
		t.Errorf("sensor = %v, want s1", input["sensor"])
	}
}

func TestMQTTMessageParsingRaw(t *testing.T) {
	msg := &mockMessage{
		topic:     "sensors/raw",
		messageID: 7,
		qos:       0,
		retained:  true,
		payload:   []byte("hello world"),
	}

	input := ParseMessage(msg)

	if input["_topic"] != "sensors/raw" {
		t.Errorf("_topic = %v, want sensors/raw", input["_topic"])
	}
	if input["_retained"] != true {
		t.Errorf("_retained = %v, want true", input["_retained"])
	}
	if raw, ok := input["_raw"].(string); !ok || raw != "hello world" {
		t.Errorf("_raw = %v, want 'hello world'", input["_raw"])
	}

	// Should not have parsed JSON fields
	if _, ok := input["temperature"]; ok {
		t.Error("unexpected parsed field 'temperature' in raw message")
	}
}

func TestMQTTHealthNotConnected(t *testing.T) {
	cfg := DefaultConfig()
	c, err := NewConnector("test_mqtt", cfg, nil)
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	err = c.Health(context.Background())
	if err == nil {
		t.Error("Health() should return error when not connected")
	}
}

func TestMQTTReadReturnsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	c, err := NewConnector("test_mqtt", cfg, nil)
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	result, err := c.Read(context.Background(), connector.Query{Target: "some/topic"})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("Read() should return empty rows, got %d", len(result.Rows))
	}
}

func TestMQTTWriteNotConnected(t *testing.T) {
	cfg := DefaultConfig()
	c, err := NewConnector("test_mqtt", cfg, nil)
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	_, err = c.Write(context.Background(), &connector.Data{
		Target:  "test/topic",
		Payload: map[string]interface{}{"key": "value"},
	})
	if err == nil {
		t.Error("Write() should return error when not connected")
	}
}

func TestMQTTQoSValidation(t *testing.T) {
	tests := []struct {
		name    string
		qos     byte
		wantErr bool
	}{
		{"QoS 0", 0, false},
		{"QoS 1", 1, false},
		{"QoS 2", 2, false},
		{"QoS 3 invalid", 3, true},
		{"QoS 255 invalid", 255, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Broker:   "tcp://localhost:1883",
				ClientID: "test",
				QoS:      tt.qos,
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMQTTFactorySupports(t *testing.T) {
	f := NewFactory(nil)

	if !f.Supports("mqtt", "") {
		t.Error("factory should support type=mqtt driver=''")
	}
	if !f.Supports("mqtt", "anything") {
		t.Error("factory should support type=mqtt with any driver")
	}
	if f.Supports("mq", "") {
		t.Error("factory should not support type=mq")
	}
	if f.Supports("rest", "") {
		t.Error("factory should not support type=rest")
	}
}

func TestMQTTFactoryCreate(t *testing.T) {
	f := NewFactory(nil)

	cfg := &connector.Config{
		Name: "test_mqtt",
		Type: "mqtt",
		Properties: map[string]interface{}{
			"broker":        "tcp://broker.example.com:1883",
			"client_id":     "my-client",
			"username":      "user",
			"password":      "pass",
			"qos":           1,
			"clean_session": false,
			"keep_alive":    "60s",
			"topic":         "default/topic",
			"tls": map[string]interface{}{
				"enabled":              true,
				"insecure_skip_verify": true,
			},
		},
	}

	conn, err := f.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	mqttConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("expected *Connector")
	}

	if mqttConn.Name() != "test_mqtt" {
		t.Errorf("Name() = %s, want test_mqtt", mqttConn.Name())
	}
	if mqttConn.config.Broker != "tcp://broker.example.com:1883" {
		t.Errorf("Broker = %s, want tcp://broker.example.com:1883", mqttConn.config.Broker)
	}
	if mqttConn.config.ClientID != "my-client" {
		t.Errorf("ClientID = %s, want my-client", mqttConn.config.ClientID)
	}
	if mqttConn.config.Username != "user" {
		t.Errorf("Username = %s, want user", mqttConn.config.Username)
	}
	if mqttConn.config.QoS != 1 {
		t.Errorf("QoS = %d, want 1", mqttConn.config.QoS)
	}
	if mqttConn.config.CleanSession {
		t.Error("CleanSession should be false")
	}
	if mqttConn.config.KeepAlive != 60*time.Second {
		t.Errorf("KeepAlive = %s, want 60s", mqttConn.config.KeepAlive)
	}
	if mqttConn.config.Topic != "default/topic" {
		t.Errorf("Topic = %s, want default/topic", mqttConn.config.Topic)
	}
	if mqttConn.config.TLS == nil || !mqttConn.config.TLS.Enabled {
		t.Error("TLS should be enabled")
	}
	if mqttConn.config.TLS != nil && !mqttConn.config.TLS.InsecureSkipVerify {
		t.Error("TLS InsecureSkipVerify should be true")
	}
}

func TestMQTTFactoryCreateInvalidQoS(t *testing.T) {
	f := NewFactory(nil)

	cfg := &connector.Config{
		Name: "bad_mqtt",
		Type: "mqtt",
		Properties: map[string]interface{}{
			"broker": "tcp://localhost:1883",
			"qos":    5,
		},
	}

	_, err := f.Create(context.Background(), cfg)
	if err == nil {
		t.Error("Create() should fail with invalid QoS")
	}
}

func TestMQTTTopicParsing(t *testing.T) {
	// Verify MQTT topic patterns are stored correctly
	cfg := DefaultConfig()
	c, err := NewConnector("test_mqtt", cfg, nil)
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	topics := []string{
		"sensors/temperature",
		"sensors/+/data",
		"sensors/#",
		"home/+/+/status",
		"$SYS/broker/uptime",
	}

	for _, topic := range topics {
		c.RegisterRoute(topic, func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return nil, nil
		})
	}

	handlers := c.TopicHandlers()
	if len(handlers) != len(topics) {
		t.Errorf("expected %d handlers, got %d", len(topics), len(handlers))
	}

	for _, topic := range topics {
		if _, ok := handlers[topic]; !ok {
			t.Errorf("handler for topic %q not found", topic)
		}
	}
}

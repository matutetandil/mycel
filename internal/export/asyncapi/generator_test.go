package asyncapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/validate"
)

func TestGenerator_Generate(t *testing.T) {
	config := &parser.Configuration{
		ServiceConfig: &parser.ServiceConfig{
			Name:    "order-service",
			Version: "2.0.0",
		},
		Connectors: []*connector.Config{
			{
				Name: "rabbitmq",
				Type: "mq",
				Properties: map[string]interface{}{
					"driver": "rabbitmq",
					"host":   "localhost",
					"port":   5672,
				},
			},
		},
		Flows: []*flow.Config{
			{
				Name: "process_order",
				From: &flow.FromConfig{
					Connector:       "rabbitmq",
					ConnectorParams: map[string]interface{}{"operation": "orders.created"},
				},
				To: &flow.ToConfig{
					Connector:       "db",
					ConnectorParams: map[string]interface{}{"target": "orders"},
				},
			},
			{
				Name: "notify_shipment",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "POST /shipments"},
				},
				To: &flow.ToConfig{
					Connector:       "rabbitmq",
					ConnectorParams: map[string]interface{}{"target": "shipments.ready"},
				},
			},
		},
		Types: []*validate.TypeSchema{
			{
				Name: "Order",
				Fields: []validate.FieldSchema{
					{Name: "id", Type: "string", Required: true},
					{Name: "customer_id", Type: "string", Required: true},
					{Name: "total", Type: "number", Required: true},
					{Name: "status", Type: "string"},
				},
			},
		},
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check basic info
	if spec.AsyncAPI != "2.6.0" {
		t.Errorf("AsyncAPI = %v, want 2.6.0", spec.AsyncAPI)
	}
	if spec.Info.Title != "order-service Events" {
		t.Errorf("Info.Title = %v, want order-service Events", spec.Info.Title)
	}
	if spec.Info.Version != "2.0.0" {
		t.Errorf("Info.Version = %v, want 2.0.0", spec.Info.Version)
	}

	// Check server
	if len(spec.Servers) != 1 {
		t.Errorf("len(Servers) = %d, want 1", len(spec.Servers))
	}
	if server, ok := spec.Servers["rabbitmq"]; ok {
		if server.Protocol != "amqp" {
			t.Errorf("Server.Protocol = %v, want amqp", server.Protocol)
		}
		if server.URL != "localhost:5672" {
			t.Errorf("Server.URL = %v, want localhost:5672", server.URL)
		}
	} else {
		t.Error("expected rabbitmq server")
	}

	// Check channels
	if len(spec.Channels) != 2 {
		t.Errorf("len(Channels) = %d, want 2", len(spec.Channels))
	}

	// Check subscribe channel
	if channel, ok := spec.Channels["orders.created"]; ok {
		if channel.Subscribe == nil {
			t.Error("expected subscribe operation on orders.created")
		} else {
			if channel.Subscribe.OperationID != "process_order" {
				t.Errorf("OperationID = %v, want process_order", channel.Subscribe.OperationID)
			}
		}
	} else {
		t.Error("expected orders.created channel")
	}

	// Check publish channel
	if channel, ok := spec.Channels["shipments.ready"]; ok {
		if channel.Publish == nil {
			t.Error("expected publish operation on shipments.ready")
		} else {
			if channel.Publish.OperationID != "notify_shipment" {
				t.Errorf("OperationID = %v, want notify_shipment", channel.Publish.OperationID)
			}
		}
	} else {
		t.Error("expected shipments.ready channel")
	}

	// Check components
	if spec.Components == nil || len(spec.Components.Schemas) != 1 {
		t.Error("expected 1 schema in components")
	}
	orderSchema, ok := spec.Components.Schemas["Order"]
	if !ok {
		t.Fatal("expected Order schema")
	}
	if len(orderSchema.Properties) != 4 {
		t.Errorf("expected 4 properties, got %d", len(orderSchema.Properties))
	}
	if len(orderSchema.Required) != 3 {
		t.Errorf("expected 3 required fields, got %d", len(orderSchema.Required))
	}
}

func TestGenerator_KafkaServer(t *testing.T) {
	config := &parser.Configuration{
		Connectors: []*connector.Config{
			{
				Name: "kafka",
				Type: "mq",
				Properties: map[string]interface{}{
					"driver":  "kafka",
					"brokers": "broker1:9092,broker2:9092",
				},
			},
		},
		Flows: []*flow.Config{
			{
				Name: "consume_events",
				From: &flow.FromConfig{
					Connector:       "kafka",
					ConnectorParams: map[string]interface{}{"operation": "events"},
				},
				To: &flow.ToConfig{
					Connector:       "db",
					ConnectorParams: map[string]interface{}{"target": "events"},
				},
			},
		},
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if server, ok := spec.Servers["kafka"]; ok {
		if server.Protocol != "kafka" {
			t.Errorf("Server.Protocol = %v, want kafka", server.Protocol)
		}
		if server.URL != "broker1:9092,broker2:9092" {
			t.Errorf("Server.URL = %v, want broker1:9092,broker2:9092", server.URL)
		}
	} else {
		t.Error("expected kafka server")
	}
}

func TestSpec_ToJSON(t *testing.T) {
	spec := &Spec{
		AsyncAPI: "2.6.0",
		Info: Info{
			Title:   "Test Events",
			Version: "1.0.0",
		},
		Channels: map[string]Channel{
			"test.topic": {
				Subscribe: &Operation{
					Summary: "Test subscription",
				},
			},
		},
	}

	jsonBytes, err := spec.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed["asyncapi"] != "2.6.0" {
		t.Errorf("asyncapi = %v, want 2.6.0", parsed["asyncapi"])
	}
}

func TestSpec_ToYAML(t *testing.T) {
	spec := &Spec{
		AsyncAPI: "2.6.0",
		Info: Info{
			Title:   "Test Events",
			Version: "1.0.0",
		},
		Channels: make(map[string]Channel),
	}

	yamlBytes, err := spec.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML() error = %v", err)
	}

	yamlStr := string(yamlBytes)
	if !strings.Contains(yamlStr, "asyncapi: 2.6.0") {
		t.Error("expected asyncapi: 2.6.0 in YAML output")
	}
	if !strings.Contains(yamlStr, "title: Test Events") {
		t.Error("expected title: Test Events in YAML output")
	}
}

func TestFormatSummary(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"process_order", "Process Order"},
		{"get_users", "Get Users"},
		{"send_notification_email", "Send Notification Email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSummary(tt.name)
			if got != tt.want {
				t.Errorf("formatSummary(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

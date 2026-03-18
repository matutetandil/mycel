package asyncapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/validate"
	"gopkg.in/yaml.v3"
)

// Generator generates AsyncAPI specifications from Mycel configuration.
type Generator struct {
	config *parser.Configuration
	types  map[string]*validate.TypeSchema
}

// NewGenerator creates a new AsyncAPI generator.
func NewGenerator(config *parser.Configuration) *Generator {
	types := make(map[string]*validate.TypeSchema)
	for _, t := range config.Types {
		types[t.Name] = t
	}

	return &Generator{
		config: config,
		types:  types,
	}
}

// Generate creates an AsyncAPI 2.6 specification.
func (g *Generator) Generate() (*Spec, error) {
	spec := &Spec{
		AsyncAPI: "2.6.0",
		Info: Info{
			Title:   "Mycel API",
			Version: "1.0.0",
		},
		Servers:  make(map[string]Server),
		Channels: make(map[string]Channel),
		Components: &Components{
			Schemas:  make(map[string]*Schema),
			Messages: make(map[string]*Message),
		},
	}

	// Set service info
	if g.config.ServiceConfig != nil {
		if g.config.ServiceConfig.Name != "" {
			spec.Info.Title = g.config.ServiceConfig.Name + " Events"
		}
		if g.config.ServiceConfig.Version != "" {
			spec.Info.Version = g.config.ServiceConfig.Version
		}
	}

	// Add servers from message queue connectors
	for _, conn := range g.config.Connectors {
		if conn.Type == "mq" {
			server := g.connectorToServer(conn)
			if server != nil {
				spec.Servers[conn.Name] = *server
			}
		}
	}

	// Generate channels from flows
	for _, f := range g.config.Flows {
		if err := g.addFlowToSpec(spec, f); err != nil {
			return nil, fmt.Errorf("processing flow %s: %w", f.Name, err)
		}
	}

	// Generate component schemas from types
	for _, t := range g.config.Types {
		schema := g.typeToSchema(t)
		spec.Components.Schemas[t.Name] = schema
	}

	return spec, nil
}

// connectorToServer converts an MQ connector to an AsyncAPI server.
func (g *Generator) connectorToServer(conn *connector.Config) *Server {
	props := conn.Properties
	driver := ""
	if d, ok := props["driver"].(string); ok {
		driver = d
	}

	switch driver {
	case "rabbitmq", "amqp":
		host := getStringProp(props, "host", "localhost")
		port := getIntProp(props, "port", 5672)
		return &Server{
			URL:      fmt.Sprintf("%s:%d", host, port),
			Protocol: "amqp",
			Description: "RabbitMQ server",
		}
	case "kafka":
		brokers := getStringProp(props, "brokers", "localhost:9092")
		return &Server{
			URL:      brokers,
			Protocol: "kafka",
			Description: "Kafka cluster",
		}
	}

	return nil
}

// addFlowToSpec adds a flow as an AsyncAPI channel if it uses MQ.
func (g *Generator) addFlowToSpec(spec *Spec, f *flow.Config) error {
	// Check if this flow involves MQ
	fromConnector := g.getConnectorType(f.From.Connector)
	toConnector := g.getConnectorType(f.To.Connector)

	// Flow consumes from MQ (subscribe)
	if fromConnector == "mq" {
		channel := g.createSubscribeChannel(f)
		// For MQ, the channel name is typically in the operation (e.g., "topic:orders")
		channelName := f.From.GetOperation()
		spec.Channels[channelName] = channel
	}

	// Flow publishes to MQ (publish)
	if toConnector == "mq" {
		// For MQ destinations, the channel name is in Target
		channelName := f.To.GetTarget()

		// Merge with existing channel if it exists
		if existing, ok := spec.Channels[channelName]; ok {
			existing.Publish = g.createPublishOperation(f)
			spec.Channels[channelName] = existing
		} else {
			channel := g.createPublishChannel(f)
			spec.Channels[channelName] = channel
		}
	}

	return nil
}

// getConnectorType returns the type of a connector by name.
func (g *Generator) getConnectorType(name string) string {
	for _, conn := range g.config.Connectors {
		if conn.Name == name {
			return conn.Type
		}
	}
	return ""
}

// createSubscribeChannel creates a channel for consuming messages.
func (g *Generator) createSubscribeChannel(f *flow.Config) Channel {
	return Channel{
		Description: formatDescription(f.Name, "subscribe"),
		Subscribe: &Operation{
			OperationID: f.Name,
			Summary:     formatSummary(f.Name),
			Message: &Message{
				Name:        f.Name + "_message",
				ContentType: "application/json",
				Payload:     g.inferMessageSchema(f),
			},
		},
		Bindings: g.inferChannelBindings(f.From.Connector),
	}
}

// createPublishChannel creates a channel for publishing messages.
func (g *Generator) createPublishChannel(f *flow.Config) Channel {
	return Channel{
		Description: formatDescription(f.Name, "publish"),
		Publish:     g.createPublishOperation(f),
		Bindings:    g.inferChannelBindings(f.To.Connector),
	}
}

// createPublishOperation creates a publish operation.
func (g *Generator) createPublishOperation(f *flow.Config) *Operation {
	return &Operation{
		OperationID: f.Name,
		Summary:     formatSummary(f.Name),
		Message: &Message{
			Name:        f.Name + "_message",
			ContentType: "application/json",
			Payload:     g.inferMessageSchema(f),
		},
	}
}

// inferMessageSchema infers the message schema from flow configuration.
func (g *Generator) inferMessageSchema(f *flow.Config) *Schema {
	// Check if flow has a validation schema
	if f.Validate != nil && f.Validate.Input != "" {
		if _, ok := g.types[f.Validate.Input]; ok {
			return &Schema{
				Ref: "#/components/schemas/" + f.Validate.Input,
			}
		}
	}

	// Check if flow has a returns type
	if f.Returns != "" {
		if _, ok := g.types[f.Returns]; ok {
			return &Schema{
				Ref: "#/components/schemas/" + f.Returns,
			}
		}
	}

	// Default to generic object
	return &Schema{Type: "object"}
}

// inferChannelBindings infers protocol bindings from connector.
func (g *Generator) inferChannelBindings(connectorName string) *ChannelBindings {
	for _, conn := range g.config.Connectors {
		if conn.Name == connectorName && conn.Type == "mq" {
			driver := ""
			if d, ok := conn.Properties["driver"].(string); ok {
				driver = d
			}

			switch driver {
			case "rabbitmq", "amqp":
				return &ChannelBindings{
					AMQP: &AMQPChannelBinding{
						Is: "queue",
					},
				}
			case "kafka":
				return &ChannelBindings{
					Kafka: &KafkaChannelBinding{},
				}
			}
		}
	}
	return nil
}

// typeToSchema converts a Mycel type schema to AsyncAPI schema.
func (g *Generator) typeToSchema(t *validate.TypeSchema) *Schema {
	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	for _, field := range t.Fields {
		propSchema := g.fieldToSchema(&field)
		schema.Properties[field.Name] = propSchema

		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
	}

	return schema
}

// fieldToSchema converts a field schema to AsyncAPI schema.
func (g *Generator) fieldToSchema(f *validate.FieldSchema) *Schema {
	schema := &Schema{}

	switch f.Type {
	case "string":
		schema.Type = "string"
	case "number":
		schema.Type = "number"
	case "integer":
		schema.Type = "integer"
	case "boolean":
		schema.Type = "boolean"
	case "array":
		schema.Type = "array"
		schema.Items = &Schema{Type: "object"}
	case "object":
		schema.Type = "object"
	default:
		// Could be a reference to another type
		if _, ok := g.types[f.Type]; ok {
			schema.Ref = "#/components/schemas/" + f.Type
		} else {
			schema.Type = "string"
		}
	}

	return schema
}

// formatSummary formats a flow name into a readable summary.
func formatSummary(name string) string {
	words := strings.Split(name, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// formatDescription creates a description for a channel.
func formatDescription(name, operation string) string {
	return fmt.Sprintf("Channel for %s (%s)", formatSummary(name), operation)
}

// getStringProp safely gets a string property.
func getStringProp(props map[string]interface{}, key, defaultVal string) string {
	if v, ok := props[key].(string); ok {
		return v
	}
	return defaultVal
}

// getIntProp safely gets an int property.
func getIntProp(props map[string]interface{}, key string, defaultVal int) int {
	if v, ok := props[key].(int); ok {
		return v
	}
	if v, ok := props[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

// ToJSON serializes the spec to JSON.
func (s *Spec) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// ToYAML serializes the spec to YAML.
func (s *Spec) ToYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

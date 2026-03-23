package mqtt

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for MQTT.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "broker", Doc: "MQTT broker URL", Type: schema.TypeString, Required: true},
			{Name: "client_id", Doc: "MQTT client identifier", Type: schema.TypeString},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "topic", Doc: "Default MQTT topic", Type: schema.TypeString},
			{Name: "qos", Doc: "Quality of Service level (0, 1, 2)", Type: schema.TypeNumber},
			{Name: "clean_session", Doc: "Start with a clean session", Type: schema.TypeBool},
			{Name: "keep_alive", Doc: "Keep-alive interval", Type: schema.TypeDuration},
			{Name: "connect_timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
			{Name: "auto_reconnect", Doc: "Enable automatic reconnection", Type: schema.TypeBool},
			{Name: "max_reconnect_interval", Doc: "Maximum interval between reconnection attempts", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
				{Name: "cert", Doc: "Client certificate file", Type: schema.TypeString},
				{Name: "key", Doc: "Client key file", Type: schema.TypeString},
				{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
			}},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "MQTT topic to subscribe to", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

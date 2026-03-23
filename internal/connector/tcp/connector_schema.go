package tcp

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for TCP.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "TCP server port", Type: schema.TypeNumber, Required: true},
			{Name: "host", Doc: "TCP server hostname", Type: schema.TypeString},
			{Name: "protocol", Doc: "Wire protocol", Type: schema.TypeString, Values: []string{"json", "msgpack", "nestjs"}},
			{Name: "max_connections", Doc: "Maximum concurrent connections", Type: schema.TypeNumber},
			{Name: "read_timeout", Doc: "Read timeout duration", Type: schema.TypeDuration},
			{Name: "write_timeout", Doc: "Write timeout duration", Type: schema.TypeDuration},
			{Name: "pool_size", Doc: "Connection pool size", Type: schema.TypeNumber},
			{Name: "connect_timeout", Doc: "Connection timeout duration", Type: schema.TypeDuration},
			{Name: "idle_timeout", Doc: "Idle connection timeout", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
				{Name: "cert", Doc: "TLS certificate file", Type: schema.TypeString},
				{Name: "key", Doc: "TLS key file", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
				{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
			}},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "TCP message pattern to handle", Type: schema.TypeString, Required: true},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block { return nil }

package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// TCPSchema implements ConnectorSchemaProvider for TCP.
type TCPSchema struct{}

func (TCPSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Which half of the connector this is: a server that listens, or a client that connects", Type: schema.TypeString, Values: []string{"server", "client"}, Default: "server"},
			{Name: "port", Doc: "TCP server port", Type: schema.TypeNumber, Required: true},
			{Name: "host", Doc: "TCP server hostname", Type: schema.TypeString},
			{Name: "protocol", Doc: "Wire protocol", Type: schema.TypeString, Values: []string{"json", "msgpack", "nestjs"}},
			{Name: "max_connections", Doc: "Maximum concurrent connections", Type: schema.TypeNumber},
			{Name: "read_timeout", Doc: "Read timeout duration", Type: schema.TypeDuration},
			{Name: "write_timeout", Doc: "Write timeout duration", Type: schema.TypeDuration},
			{Name: "pool_size", Doc: "Connection pool size", Type: schema.TypeNumber},
			{Name: "connect_timeout", Doc: "Connection timeout duration", Type: schema.TypeDuration},
			{Name: "idle_timeout", Doc: "Idle connection timeout", Type: schema.TypeDuration},
			{Name: "retry_delay", Doc: "Wait between reconnection attempts (client)", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS (default true when the block is present)", Type: schema.TypeBool},
				{Name: "cert", Doc: "Certificate file this connector presents: its own when it is a server, the client certificate for mutual TLS", Type: schema.TypeString},
				{Name: "key", Doc: "Private key for cert", Type: schema.TypeString},
				{Name: "ca_cert", Doc: "CA certificate file used to verify the other side", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification (development only)", Type: schema.TypeBool},
			}},
		},
	}
}

func (TCPSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "TCP message pattern to handle", Type: schema.TypeString, Required: true},
		},
	}
}

func (TCPSchema) TargetSchema() *schema.Block { return nil }

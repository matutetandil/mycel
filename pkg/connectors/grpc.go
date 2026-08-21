package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// GRPCSchema implements ConnectorSchemaProvider for gRPC.
type GRPCSchema struct{}

func (GRPCSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			// Both are read by the factory and were described by nothing, so
			// completions and `mycel add` did not know they existed.
			{Name: "timeout", Doc: "How long a call may take", Type: schema.TypeDuration},
			{Name: "host", Doc: "gRPC server hostname", Type: schema.TypeString},
			{Name: "port", Doc: "gRPC server port", Type: schema.TypeNumber},
			{Name: "proto_path", Doc: "Path to .proto file or directory", Type: schema.TypeString},
			{Name: "reflection", Doc: "Enable gRPC server reflection", Type: schema.TypeBool},
			{Name: "max_recv_mb", Doc: "Maximum receive message size in MB", Type: schema.TypeNumber},
			{Name: "max_send_mb", Doc: "Maximum send message size in MB", Type: schema.TypeNumber},
			{Name: "proto_files", Doc: "List of proto file paths", Type: schema.TypeList},
			{Name: "target", Doc: "Address of the service to call (client)", Type: schema.TypeString},
			{Name: "insecure", Doc: "Connect without TLS (client, development only)", Type: schema.TypeBool},
			{Name: "wait_for_ready", Doc: "Queue calls until the connection is ready instead of failing fast (client)", Type: schema.TypeBool},
		},
		Children: []schema.Block{
			// Read by the factory and described by nothing until now, so
			// completions and `mycel add` did not know it existed.
			{Type: "keep_alive", Doc: "Keep the connection open between calls", Attrs: []schema.Attr{
				{Name: "time", Doc: "How often to ping an idle connection", Type: schema.TypeDuration},
				{Name: "timeout", Doc: "How long to wait for the answer to a ping", Type: schema.TypeDuration},
			}},
			{Type: "tls", Doc: "TLS/SSL settings. Writing the block enables TLS; set enabled = false to turn it off without removing it", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS (default true when the block is present)", Type: schema.TypeBool},
				{Name: "cert", Doc: "Certificate file this connector presents: its own when it is a server, the client certificate for mutual TLS", Type: schema.TypeString},
				{Name: "key", Doc: "Private key for cert", Type: schema.TypeString},
				{Name: "ca_cert", Doc: "CA certificate file used to verify the other side", Type: schema.TypeString},
				{Name: "server_name", Doc: "Expected server name, overriding the address used to connect (SNI)", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification (development only)", Type: schema.TypeBool},
			}},
			{Type: "auth", Doc: "Authentication settings", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type", Type: schema.TypeString},
				{Name: "public", Doc: "Public (unauthenticated) methods", Type: schema.TypeList},
			}},
		},
	}
}

func (GRPCSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "gRPC method to expose (e.g., GetUser)", Type: schema.TypeString, Required: true},
		},
	}
}

func (GRPCSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "gRPC method to call", Type: schema.TypeString},
		},
	}
}

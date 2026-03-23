package grpc

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for gRPC.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "gRPC server hostname", Type: schema.TypeString},
			{Name: "port", Doc: "gRPC server port", Type: schema.TypeNumber},
			{Name: "proto_path", Doc: "Path to .proto file or directory", Type: schema.TypeString},
			{Name: "reflection", Doc: "Enable gRPC server reflection", Type: schema.TypeBool},
			{Name: "max_recv_mb", Doc: "Maximum receive message size in MB", Type: schema.TypeNumber},
			{Name: "max_send_mb", Doc: "Maximum send message size in MB", Type: schema.TypeNumber},
			{Name: "proto_files", Doc: "List of proto file paths", Type: schema.TypeList},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
				{Name: "cert_file", Doc: "TLS certificate file", Type: schema.TypeString},
				{Name: "key_file", Doc: "TLS key file", Type: schema.TypeString},
				{Name: "ca_file", Doc: "CA certificate file", Type: schema.TypeString},
				{Name: "server_name", Doc: "TLS server name override", Type: schema.TypeString},
				{Name: "skip_verify", Doc: "Skip TLS certificate verification", Type: schema.TypeBool},
			}},
			{Type: "auth", Doc: "Authentication settings", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type", Type: schema.TypeString},
				{Name: "public", Doc: "Public (unauthenticated) methods", Type: schema.TypeList},
			}},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "gRPC method to expose (e.g., GetUser)", Type: schema.TypeString, Required: true},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "gRPC method to call", Type: schema.TypeString},
		},
	}
}

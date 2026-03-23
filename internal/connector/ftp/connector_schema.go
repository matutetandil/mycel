package ftp

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for FTP/SFTP.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "FTP/SFTP server hostname", Type: schema.TypeString, Required: true},
			{Name: "port", Doc: "Server port", Type: schema.TypeNumber},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "protocol", Doc: "Transfer protocol", Type: schema.TypeString, Values: []string{"ftp", "sftp"}},
			{Name: "base_path", Doc: "Base directory on remote server", Type: schema.TypeString},
			{Name: "key_file", Doc: "SSH private key file for SFTP", Type: schema.TypeString},
			{Name: "passive", Doc: "Use passive mode for FTP", Type: schema.TypeBool},
			{Name: "tls", Doc: "Enable TLS for FTP", Type: schema.TypeBool},
			{Name: "timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

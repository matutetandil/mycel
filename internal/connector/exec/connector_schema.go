package exec

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for Exec.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "command", Doc: "Command to execute", Type: schema.TypeString, Required: true},
			{Name: "workdir", Doc: "Working directory for the command", Type: schema.TypeString},
			{Name: "timeout", Doc: "Execution timeout", Type: schema.TypeDuration},
			{Name: "shell", Doc: "Shell to use (e.g., /bin/sh)", Type: schema.TypeString},
			{Name: "input_format", Doc: "Input data format", Type: schema.TypeString},
			{Name: "output_format", Doc: "Output data format", Type: schema.TypeString},
		},
		Children: []schema.Block{
			{Type: "env", Doc: "Environment variables", Open: true},
			{Type: "ssh", Doc: "SSH remote execution settings", Attrs: []schema.Attr{
				{Name: "host", Doc: "SSH server hostname", Type: schema.TypeString},
				{Name: "port", Doc: "SSH server port", Type: schema.TypeNumber},
				{Name: "user", Doc: "SSH username", Type: schema.TypeString},
				{Name: "key_file", Doc: "SSH private key file", Type: schema.TypeString},
				{Name: "password", Doc: "SSH password", Type: schema.TypeString},
				{Name: "known_hosts", Doc: "Known hosts file path", Type: schema.TypeString},
			}},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

package s3

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for S3.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "bucket", Doc: "S3 bucket name", Type: schema.TypeString, Required: true},
			{Name: "region", Doc: "AWS region", Type: schema.TypeString},
			{Name: "endpoint", Doc: "Custom S3 endpoint URL", Type: schema.TypeString},
			{Name: "access_key", Doc: "AWS access key ID", Type: schema.TypeString},
			{Name: "secret_key", Doc: "AWS secret access key", Type: schema.TypeString},
			{Name: "session_token", Doc: "AWS session token", Type: schema.TypeString},
			{Name: "prefix", Doc: "Key prefix for all operations", Type: schema.TypeString},
			{Name: "format", Doc: "File format", Type: schema.TypeString, Values: []string{"json", "csv", "tsv", "yaml", "xlsx"}},
			{Name: "use_path_style", Doc: "Use path-style addressing", Type: schema.TypeBool},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

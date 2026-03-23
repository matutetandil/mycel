package file

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for File.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "base_path", Doc: "Base directory path for file operations", Type: schema.TypeString, Required: true},
			{Name: "format", Doc: "File format", Type: schema.TypeString, Values: []string{"json", "csv", "tsv", "yaml", "xlsx"}},
			{Name: "watch", Doc: "Enable file watching", Type: schema.TypeBool},
			{Name: "create_dirs", Doc: "Create directories if they do not exist", Type: schema.TypeBool},
			{Name: "permissions", Doc: "File permissions (numeric, e.g., 0644)", Type: schema.TypeNumber},
			{Name: "watch_interval", Doc: "File watch polling interval", Type: schema.TypeString},
			{Name: "csv_delimiter", Doc: "CSV field delimiter", Type: schema.TypeString},
			{Name: "csv_comment", Doc: "CSV comment character", Type: schema.TypeString},
			{Name: "csv_no_header", Doc: "CSV has no header row", Type: schema.TypeBool},
			{Name: "csv_trim_space", Doc: "Trim leading space in CSV fields", Type: schema.TypeBool},
			{Name: "csv_skip_rows", Doc: "Number of rows to skip at start", Type: schema.TypeNumber},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "File operation (read, list, watch)", Type: schema.TypeString},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

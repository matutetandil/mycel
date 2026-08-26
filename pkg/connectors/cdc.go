package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// CDCSchema implements ConnectorSchemaProvider for CDC (Change Data Capture).
type CDCSchema struct{}

func (CDCSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Database whose changes are streamed", Type: schema.TypeString, Values: []string{"postgres"}, Required: true},
			{Name: "host", Doc: "PostgreSQL host", Type: schema.TypeString, Required: true},
			{Name: "port", Doc: "PostgreSQL port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
			{Name: "user", Doc: "Database user", Type: schema.TypeString, Required: true},
			{Name: "password", Doc: "Database password", Type: schema.TypeString},
			{Name: "sslmode", Doc: "SSL mode (disable, require, verify-full)", Type: schema.TypeString},
			{Name: "slot_name", Doc: "Replication slot name", Type: schema.TypeString},
			{Name: "publication", Doc: "PostgreSQL publication name", Type: schema.TypeString},
		},
	}
}

func (CDCSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "Table or event filter", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (CDCSchema) TargetSchema() *schema.Block { return nil }

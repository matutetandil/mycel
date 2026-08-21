package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// ElasticsearchSchema implements ConnectorSchemaProvider for Elasticsearch.
type ElasticsearchSchema struct{}

func (ElasticsearchSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "url", Doc: "Elasticsearch URL", Type: schema.TypeString, Required: true},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "index", Doc: "Default index name", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "nodes", Doc: "Additional cluster node URLs", Type: schema.TypeList},
		},
	}
}

func (ElasticsearchSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (ElasticsearchSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "Elasticsearch operation", Type: schema.TypeString, Values: []string{"index", "bulk", "delete"}},
		},
	}
}

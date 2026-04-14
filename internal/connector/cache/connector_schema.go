package cache

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for Cache.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Cache backend driver", Type: schema.TypeString, Required: true, Values: []string{"memory", "redis"}},
			{Name: "mode", Doc: "Cache mode", Type: schema.TypeString},
			{Name: "url", Doc: "Redis connection URL", Type: schema.TypeString},
			{Name: "host", Doc: "Redis host (alternative to url)", Type: schema.TypeString},
			{Name: "port", Doc: "Redis port (default: 6379)", Type: schema.TypeNumber},
			{Name: "password", Doc: "Redis password", Type: schema.TypeString},
			{Name: "db", Doc: "Redis database number (default: 0)", Type: schema.TypeNumber},
			{Name: "prefix", Doc: "Key prefix for namespacing", Type: schema.TypeString},
			{Name: "max_items", Doc: "Maximum number of cached items", Type: schema.TypeNumber},
			{Name: "eviction", Doc: "Eviction policy", Type: schema.TypeString},
			{Name: "default_ttl", Doc: "Default time-to-live for entries", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "pool", Doc: "Connection pool settings", Attrs: []schema.Attr{
				{Name: "max_connections", Doc: "Maximum pool connections", Type: schema.TypeNumber},
				{Name: "min_idle", Doc: "Minimum idle connections", Type: schema.TypeNumber},
				{Name: "max_idle_time", Doc: "Maximum idle time before eviction", Type: schema.TypeDuration},
				{Name: "connect_timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
			}},
			{Type: "cluster", Doc: "Redis cluster settings", Open: true, Attrs: []schema.Attr{
				{Name: "nodes", Doc: "Cluster node addresses", Type: schema.TypeList},
				{Name: "password", Doc: "Cluster password", Type: schema.TypeString},
			}},
			{Type: "sentinel", Doc: "Redis sentinel settings", Open: true, Attrs: []schema.Attr{
				{Name: "master_name", Doc: "Sentinel master name", Type: schema.TypeString},
				{Name: "nodes", Doc: "Sentinel node addresses", Type: schema.TypeList},
				{Name: "password", Doc: "Sentinel password", Type: schema.TypeString},
			}},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }
func (ConnectorSchemaDef) TargetSchema() *schema.Block  { return nil }

package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// GraphQLSchema implements ConnectorSchemaProvider for GraphQL.
type GraphQLSchema struct{}

func (GraphQLSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Which half of the connector this is: a server that serves GraphQL, or a client that calls one", Type: schema.TypeString, Values: []string{"server", "client"}, Default: "server"},
			{Name: "port", Doc: "GraphQL server port", Type: schema.TypeNumber},
			{Name: "host", Doc: "Server host or client endpoint", Type: schema.TypeString},
			{Name: "endpoint", Doc: "Client endpoint URL", Type: schema.TypeString},
			{Name: "playground", Doc: "Enable GraphiQL playground", Type: schema.TypeBool},
			{Name: "introspection", Doc: "Allow clients to query the schema itself. Off in production by default", Type: schema.TypeBool},
			{Name: "playground_path", Doc: "Playground URL path", Type: schema.TypeString},
			{Name: "timeout", Doc: "Client request timeout", Type: schema.TypeDuration},
			{Name: "retry_count", Doc: "How many times a client retries a failed request", Type: schema.TypeNumber},
			{Name: "retry_delay", Doc: "How long a client waits between retries", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "schema", Doc: "Schema configuration", Attrs: []schema.Attr{
				{Name: "path", Doc: "Schema file path", Type: schema.TypeString},
				{Name: "auto_generate", Doc: "Auto-generate from flows", Type: schema.TypeBool},
			}},
			{Type: "cors", Doc: "CORS settings", Attrs: []schema.Attr{
				{Name: "origins", Doc: "Allowed origins", Type: schema.TypeList},
				{Name: "methods", Doc: "Allowed methods", Type: schema.TypeList},
				{Name: "headers", Doc: "Allowed headers", Type: schema.TypeList},
				{Name: "allow_credentials", Doc: "Allow credentials", Type: schema.TypeBool},
			}},
			{Type: "federation", Doc: "Federation v2", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable Federation v2", Type: schema.TypeBool},
				{Name: "version", Doc: "Federation version", Type: schema.TypeNumber},
			}},
			{Type: "subscriptions", Doc: "WebSocket subscriptions", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable subscriptions", Type: schema.TypeBool},
				{Name: "path", Doc: "WebSocket path", Type: schema.TypeString},
				{Name: "keep_alive_interval", Doc: "Ping period on an idle socket (default 30s)", Type: schema.TypeDuration},
				{Name: "connection_timeout", Doc: "Drop a connection that stops answering (default 60s)", Type: schema.TypeDuration},
			}},
			{Type: "headers", Doc: "Default client headers", Open: true},
			{Type: "auth", Doc: "Authentication", Open: true},
		},
	}
}

func (GraphQLSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "Query.name / Mutation.name / Subscription.name", Type: schema.TypeString, Required: true},
		},
	}
}

func (GraphQLSchema) TargetSchema() *schema.Block {
	return &schema.Block{Open: true, Attrs: []schema.Attr{
		{Name: "operation", Doc: "Target operation", Type: schema.TypeString},
	}}
}

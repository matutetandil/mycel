package ide

import "github.com/matutetandil/mycel/pkg/schema"

// connectorTypeAttrsFromRegistry returns connector-type-specific attributes
// from the registry. Returns nil if not found.
func connectorTypeAttrsFromRegistry(reg *schema.Registry, connType, driver string) []AttrSchema {
	if reg == nil {
		return nil
	}
	p := reg.Lookup(connType, driver)
	if p == nil {
		return nil
	}
	return p.ConnectorSchema().Attrs
}

// connectorTypeChildrenFromRegistry returns connector-type-specific child blocks
// from the registry. Returns nil if not found.
func connectorTypeChildrenFromRegistry(reg *schema.Registry, connType, driver string) []BlockSchema {
	if reg == nil {
		return nil
	}
	p := reg.Lookup(connType, driver)
	if p == nil {
		return nil
	}
	return p.ConnectorSchema().Children
}

// connectorTypeAttrs returns additional attributes for a specific connector type.
// Uses the registry if available, otherwise falls back to static defaults.
func connectorTypeAttrs(connType string) []AttrSchema {
	return connectorTypeAttrsStatic(connType)
}

// connectorTypeAttrsStatic is the static fallback for when no registry is available.
func connectorTypeAttrsStatic(connType string) []AttrSchema {
	switch connType {
	case "database":
		return []AttrSchema{
			{Name: "driver", Doc: "Database driver", Type: AttrString, Required: true, Values: []string{"sqlite", "postgres", "mysql", "mongodb"}},
			{Name: "host", Doc: "Database server host", Type: AttrString},
			{Name: "port", Doc: "Database server port", Type: AttrNumber},
			{Name: "database", Doc: "Database name or file path", Type: AttrString, Required: true},
			{Name: "user", Doc: "Database username", Type: AttrString},
			{Name: "password", Doc: "Database password", Type: AttrString},
		}
	case "rest":
		return []AttrSchema{
			{Name: "port", Doc: "HTTP server port", Type: AttrNumber, Required: true},
			{Name: "base_url", Doc: "Base URL for HTTP client mode", Type: AttrString},
		}
	case "mq":
		return []AttrSchema{
			{Name: "driver", Doc: "Message queue driver", Type: AttrString, Required: true, Values: []string{"rabbitmq", "kafka", "redis"}},
			{Name: "host", Doc: "Broker hostname", Type: AttrString},
			{Name: "port", Doc: "Broker port", Type: AttrNumber},
			{Name: "username", Doc: "Broker username", Type: AttrString},
			{Name: "password", Doc: "Broker password", Type: AttrString},
			{Name: "url", Doc: "Broker connection URL", Type: AttrString},
			{Name: "vhost", Doc: "RabbitMQ virtual host", Type: AttrString},
		}
	case "graphql":
		return []AttrSchema{
			{Name: "port", Doc: "GraphQL server port", Type: AttrNumber},
			{Name: "base_url", Doc: "GraphQL client endpoint URL", Type: AttrString},
			{Name: "playground", Doc: "Enable GraphiQL playground", Type: AttrBool},
		}
	case "grpc":
		return []AttrSchema{
			{Name: "port", Doc: "gRPC server port", Type: AttrNumber},
			{Name: "host", Doc: "gRPC client target host", Type: AttrString},
			{Name: "proto", Doc: "Proto file path", Type: AttrString},
		}
	case "cache":
		return []AttrSchema{
			{Name: "driver", Doc: "Cache driver", Type: AttrString, Required: true, Values: []string{"memory", "redis"}},
			{Name: "host", Doc: "Redis host", Type: AttrString},
			{Name: "port", Doc: "Redis port", Type: AttrNumber},
		}
	case "slack":
		return []AttrSchema{
			{Name: "token", Doc: "Slack bot OAuth token", Type: AttrString, Required: true},
			{Name: "channel", Doc: "Default Slack channel", Type: AttrString},
		}
	case "pdf":
		return []AttrSchema{
			{Name: "template", Doc: "HTML template file path", Type: AttrString, Required: true},
		}
	case "elasticsearch":
		return []AttrSchema{
			{Name: "url", Doc: "Elasticsearch URL", Type: AttrString, Required: true},
		}
	}
	return nil
}

// validateConnectorType returns diagnostics for connector-type-specific requirements.
func validateConnectorType(path string, b *Block, reg *schema.Registry) []*Diagnostic {
	connType := b.GetAttr("type")
	if connType == "" {
		return nil
	}

	driver := b.GetAttr("driver")

	// Try registry first
	typeAttrs := connectorTypeAttrsFromRegistry(reg, connType, driver)
	if typeAttrs == nil {
		// Fall back to static
		typeAttrs = connectorTypeAttrsStatic(connType)
	}

	if len(typeAttrs) == 0 {
		return nil
	}

	var diags []*Diagnostic
	for _, ta := range typeAttrs {
		if ta.Required && !b.HasAttr(ta.Name) {
			diags = append(diags, &Diagnostic{
				Severity: SeverityWarning,
				Message:  connType + " connector requires attribute " + ta.Name,
				File:     path,
				Range:    b.Range,
			})
		}
	}
	return diags
}

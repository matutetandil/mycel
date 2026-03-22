package ide

// Connector-type-aware attribute schemas.
// Returns required and recommended attributes for each connector type and driver.

// connectorTypeAttrs returns additional attributes required or recommended
// for a specific connector type value.
func connectorTypeAttrs(connType string) []AttrSchema {
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
	case "tcp":
		return []AttrSchema{
			{Name: "driver", Doc: "TCP protocol", Type: AttrString, Values: []string{"json", "msgpack", "nestjs"}},
			{Name: "host", Doc: "TCP host", Type: AttrString},
			{Name: "port", Doc: "TCP port", Type: AttrNumber, Required: true},
		}
	case "file":
		return []AttrSchema{
			{Name: "path", Doc: "File system path", Type: AttrString, Required: true},
		}
	case "s3":
		return []AttrSchema{
			{Name: "bucket", Doc: "S3 bucket name", Type: AttrString, Required: true},
			{Name: "region", Doc: "AWS region", Type: AttrString},
			{Name: "endpoint", Doc: "S3-compatible endpoint (MinIO)", Type: AttrString},
		}
	case "soap":
		return []AttrSchema{
			{Name: "port", Doc: "SOAP server port", Type: AttrNumber},
			{Name: "base_url", Doc: "SOAP client endpoint URL", Type: AttrString},
			{Name: "wsdl", Doc: "WSDL file path", Type: AttrString},
		}
	case "mqtt":
		return []AttrSchema{
			{Name: "host", Doc: "MQTT broker host", Type: AttrString, Required: true},
			{Name: "port", Doc: "MQTT broker port", Type: AttrNumber},
			{Name: "client_id", Doc: "MQTT client identifier", Type: AttrString},
		}
	case "ftp":
		return []AttrSchema{
			{Name: "driver", Doc: "FTP protocol", Type: AttrString, Values: []string{"ftp", "sftp"}},
			{Name: "host", Doc: "FTP/SFTP host", Type: AttrString, Required: true},
			{Name: "port", Doc: "FTP/SFTP port", Type: AttrNumber},
			{Name: "user", Doc: "Username", Type: AttrString},
			{Name: "password", Doc: "Password", Type: AttrString},
		}
	case "elasticsearch":
		return []AttrSchema{
			{Name: "url", Doc: "Elasticsearch URL", Type: AttrString, Required: true},
			{Name: "index", Doc: "Default index name", Type: AttrString},
		}
	case "oauth":
		return []AttrSchema{
			{Name: "driver", Doc: "OAuth provider", Type: AttrString, Required: true, Values: []string{"google", "github", "apple", "oidc"}},
			{Name: "client_id", Doc: "OAuth client ID", Type: AttrString, Required: true},
			{Name: "client_secret", Doc: "OAuth client secret", Type: AttrString, Required: true},
		}
	case "email":
		return []AttrSchema{
			{Name: "driver", Doc: "Email provider", Type: AttrString, Values: []string{"smtp", "sendgrid", "ses"}},
			{Name: "host", Doc: "SMTP host", Type: AttrString},
			{Name: "from", Doc: "Sender email address", Type: AttrString, Required: true},
		}
	case "slack":
		return []AttrSchema{
			{Name: "token", Doc: "Slack bot OAuth token", Type: AttrString, Required: true},
			{Name: "channel", Doc: "Default Slack channel", Type: AttrString},
		}
	case "discord":
		return []AttrSchema{
			{Name: "token", Doc: "Discord bot token", Type: AttrString, Required: true},
			{Name: "channel", Doc: "Default Discord channel ID", Type: AttrString},
		}
	case "pdf":
		return []AttrSchema{
			{Name: "template", Doc: "HTML template file path", Type: AttrString, Required: true},
		}
	case "cdc":
		return []AttrSchema{
			{Name: "host", Doc: "PostgreSQL host for CDC", Type: AttrString, Required: true},
			{Name: "port", Doc: "PostgreSQL port", Type: AttrNumber},
			{Name: "database", Doc: "Database name", Type: AttrString, Required: true},
			{Name: "user", Doc: "Replication user", Type: AttrString, Required: true},
			{Name: "password", Doc: "Replication password", Type: AttrString},
			{Name: "slot", Doc: "Replication slot name", Type: AttrString},
		}
	case "websocket":
		return []AttrSchema{
			{Name: "port", Doc: "WebSocket server port", Type: AttrNumber},
			{Name: "path", Doc: "WebSocket endpoint path", Type: AttrString},
		}
	case "sse":
		return []AttrSchema{
			{Name: "port", Doc: "SSE server port", Type: AttrNumber},
			{Name: "path", Doc: "SSE endpoint path", Type: AttrString},
		}
	}
	return nil
}

// validateConnectorType returns diagnostics for connector-type-specific requirements.
func validateConnectorType(path string, b *Block) []*Diagnostic {
	connType := b.GetAttr("type")
	if connType == "" {
		return nil
	}

	typeAttrs := connectorTypeAttrs(connType)
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

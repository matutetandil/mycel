package push

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for Push notifications.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Push notification driver", Type: schema.TypeString, Values: []string{"fcm", "apns"}},
			{Name: "server_key", Doc: "FCM server key (legacy)", Type: schema.TypeString},
			{Name: "project_id", Doc: "FCM project ID", Type: schema.TypeString},
			{Name: "service_account_json", Doc: "FCM service account JSON file path", Type: schema.TypeString},
			{Name: "api_url", Doc: "FCM API base URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "team_id", Doc: "APNs team ID", Type: schema.TypeString},
			{Name: "key_id", Doc: "APNs key ID", Type: schema.TypeString},
			{Name: "private_key", Doc: "APNs private key file path", Type: schema.TypeString},
			{Name: "bundle_id", Doc: "APNs app bundle ID", Type: schema.TypeString},
			{Name: "production", Doc: "Use APNs production environment", Type: schema.TypeBool},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

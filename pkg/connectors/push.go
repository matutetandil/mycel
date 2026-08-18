package connectors

import "github.com/matutetandil/mycel/v2/pkg/schema"

// PushSchema implements ConnectorSchemaProvider for Push notifications.
type PushSchema struct{}

func (PushSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Push notification driver", Type: schema.TypeString, Values: []string{"fcm", "apns"}},
			{Name: "server_key", Doc: "FCM server key. The legacy API Google retired in June 2024; configuring one is refused at startup", Type: schema.TypeString},
			{Name: "project_id", Doc: "Firebase project the messages belong to; taken from the service account when not written", Type: schema.TypeString},
			{Name: "service_account_json", Doc: "The service account from the Firebase console: a path to the file, or the JSON itself", Type: schema.TypeString},
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

func (PushSchema) SourceSchema() *schema.Block { return nil }

func (PushSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

package oauth

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for OAuth.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "OAuth provider", Type: schema.TypeString, Required: true, Values: []string{"google", "github", "apple", "oidc", "custom"}},
			{Name: "client_id", Doc: "OAuth client ID", Type: schema.TypeString, Required: true},
			{Name: "client_secret", Doc: "OAuth client secret", Type: schema.TypeString, Required: true},
			{Name: "redirect_uri", Doc: "OAuth redirect URI", Type: schema.TypeString},
			{Name: "scopes", Doc: "Requested OAuth scopes", Type: schema.TypeList},
			{Name: "team_id", Doc: "Apple team ID", Type: schema.TypeString},
			{Name: "key_id", Doc: "Apple key ID", Type: schema.TypeString},
			{Name: "private_key", Doc: "Apple private key path", Type: schema.TypeString},
			{Name: "issuer_url", Doc: "OIDC issuer URL", Type: schema.TypeString},
			{Name: "name", Doc: "Custom provider name", Type: schema.TypeString},
			{Name: "auth_url", Doc: "Custom authorization URL", Type: schema.TypeString},
			{Name: "token_url", Doc: "Custom token URL", Type: schema.TypeString},
			{Name: "userinfo_url", Doc: "Custom userinfo URL", Type: schema.TypeString},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }
func (ConnectorSchemaDef) TargetSchema() *schema.Block  { return nil }

package http

import (
	"context"
	"fmt"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates HTTP client connectors.
type Factory struct{}

// NewFactory creates a new HTTP connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "http"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "http"
}

// Create creates a new HTTP client connector from config.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	// Get base URL (required)
	baseURL, ok := cfg.Properties["base_url"].(string)
	if !ok || baseURL == "" {
		return nil, fmt.Errorf("http connector requires base_url")
	}

	// Get timeout (optional, default 30s)
	timeout := 30 * time.Second
	if t, ok := cfg.Properties["timeout"].(string); ok {
		parsed, err := time.ParseDuration(t)
		if err == nil {
			timeout = parsed
		}
	} else if t, ok := cfg.Properties["timeout"].(int); ok {
		timeout = time.Duration(t) * time.Second
	}

	// Get retry count (optional, default 1). Accepts numeric and string values
	// so retry_count = env("RETRY", "3") works.
	retryCount := connector.IntFromProps(cfg.Properties, "retry_count", 1)
	// Nested retry block takes precedence over the shorthand.
	if retry, ok := cfg.Properties["retry"].(map[string]interface{}); ok {
		if attempts, ok := connector.IntFromPropsStrict(retry, "attempts"); ok {
			retryCount = attempts
		}
	}

	// Get custom headers (optional)
	headers := make(map[string]string)
	if h, ok := cfg.Properties["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	// Parse auth config (optional)
	var auth *AuthConfig
	if authCfg, ok := cfg.Properties["auth"].(map[string]interface{}); ok {
		auth = parseAuthConfig(authCfg)
	}

	// Parse TLS config (optional)
	var tlsCfg *TLSConfig
	if tlsMap, ok := cfg.Properties["tls"].(map[string]interface{}); ok {
		tlsCfg = parseTLSConfig(tlsMap)
	}

	// Create connector with TLS
	conn := NewWithTLS(cfg.Name, baseURL, timeout, auth, tlsCfg, headers, retryCount)

	// Set format if configured (default: json)
	if format, ok := cfg.Properties["format"].(string); ok && format != "" {
		conn.SetFormat(format)
	}

	return conn, nil
}

// parseAuthConfig parses authentication configuration from HCL.
func parseAuthConfig(cfg map[string]interface{}) *AuthConfig {
	auth := &AuthConfig{
		Type: AuthTypeNone,
	}

	// Get auth type
	if t, ok := cfg["type"].(string); ok {
		switch t {
		case "bearer":
			auth.Type = AuthTypeBearer
		case "oauth2":
			auth.Type = AuthTypeOAuth2
		case "client_credentials":
			auth.Type = AuthTypeClientCredentials
		case "apikey", "api_key":
			auth.Type = AuthTypeAPIKey
		case "basic":
			auth.Type = AuthTypeBasic
		}
	}

	// Grant type (for OAuth2)
	if grantType, ok := cfg["grant_type"].(string); ok {
		auth.GrantType = grantType
		// If grant_type is client_credentials but type is oauth2, upgrade
		if grantType == "client_credentials" && auth.Type == AuthTypeOAuth2 {
			auth.Type = AuthTypeClientCredentials
		}
	}

	// Bearer token
	if token, ok := cfg["token"].(string); ok {
		auth.Token = token
	}

	// OAuth2 settings
	if refreshToken, ok := cfg["refresh_token"].(string); ok {
		auth.RefreshToken = refreshToken
	}
	if tokenURL, ok := cfg["token_url"].(string); ok {
		auth.TokenURL = tokenURL
	}
	if clientID, ok := cfg["client_id"].(string); ok {
		auth.ClientID = clientID
	}
	if clientSecret, ok := cfg["client_secret"].(string); ok {
		auth.ClientSecret = clientSecret
	}
	if scopes, ok := cfg["scopes"].([]interface{}); ok {
		for _, s := range scopes {
			if str, ok := s.(string); ok {
				auth.Scopes = append(auth.Scopes, str)
			}
		}
	}

	// API Key settings
	if apiKey, ok := cfg["api_key"].(string); ok {
		auth.APIKey = apiKey
	}
	if header, ok := cfg["api_key_header"].(string); ok {
		auth.APIKeyHeader = header
	}
	if query, ok := cfg["api_key_query"].(string); ok {
		auth.APIKeyQuery = query
	}

	// Basic auth
	if username, ok := cfg["username"].(string); ok {
		auth.Username = username
	}
	if password, ok := cfg["password"].(string); ok {
		auth.Password = password
	}

	return auth
}

// parseTLSConfig parses TLS configuration from HCL.
func parseTLSConfig(cfg map[string]interface{}) *TLSConfig {
	tls := &TLSConfig{}

	if caCert, ok := cfg["ca_cert"].(string); ok {
		tls.CACert = caCert
	}
	if clientCert, ok := cfg["client_cert"].(string); ok {
		tls.ClientCert = clientCert
	}
	if clientKey, ok := cfg["client_key"].(string); ok {
		tls.ClientKey = clientKey
	}
	if insecure, ok := cfg["insecure_skip_verify"].(bool); ok {
		tls.InsecureSkipVerify = insecure
	}

	return tls
}

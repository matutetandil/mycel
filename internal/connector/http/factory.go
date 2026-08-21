package http

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
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
	retryPolicy := DefaultRetryPolicy(retryCount)
	// Nested retry block takes precedence over the shorthand.
	if retry, ok := cfg.Properties["retry"].(map[string]interface{}); ok {
		if attempts, ok := connector.IntFromPropsStrict(retry, "attempts"); ok {
			retryCount = attempts
			retryPolicy.Attempts = attempts
		}
		if d, ok := retry["delay"].(string); ok {
			if parsed, err := time.ParseDuration(d); err == nil {
				retryPolicy.Delay = parsed
			}
		}
		if d, ok := retry["max_delay"].(string); ok {
			if parsed, err := time.ParseDuration(d); err == nil {
				retryPolicy.MaxDelay = parsed
			}
		}
		if b, ok := retry["backoff"].(string); ok {
			retryPolicy.Backoff = b
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
		var err error
		if auth, err = parseAuthConfig(authCfg); err != nil {
			return nil, fmt.Errorf("http connector %q: %w", cfg.Name, err)
		}
	}

	// Parse TLS config (optional). Writing the block is the opt-in, so it is
	// applied unless it says enabled = false — which is how the setting can be
	// driven from the environment without deleting the certificates.
	var tlsCfg *TLSConfig
	if tlsMap, ok := cfg.Properties["tls"].(map[string]interface{}); ok {
		if enabled, set := connector.BoolFromPropsStrict(tlsMap, "enabled"); !set || enabled {
			tlsCfg = parseTLSConfig(tlsMap)
		}
	}

	// Create connector with TLS
	conn := NewWithTLS(cfg.Name, baseURL, timeout, auth, tlsCfg, headers, retryCount).
		WithRetryPolicy(retryPolicy)

	// Set format if configured (default: json)
	if format, ok := cfg.Properties["format"].(string); ok && format != "" {
		conn.SetFormat(format)
	}

	return conn, nil
}

// parseAuthConfig parses authentication configuration from HCL.
//
// A type the client cannot honour is refused rather than quietly becoming no
// authentication: a connector built that way sends every request without
// credentials, and the 401s that come back look like somebody else's problem.
func parseAuthConfig(cfg map[string]interface{}) (*AuthConfig, error) {
	auth := &AuthConfig{
		Type: AuthTypeNone,
	}

	// Get auth type. Compared without regard to case, because the word is also
	// the name of an HTTP scheme and gets written the way headers spell it.
	if t, ok := cfg["type"].(string); ok && t != "" {
		switch strings.ToLower(t) {
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
		default:
			return nil, fmt.Errorf(
				"auth type %q is not one of: bearer, oauth2, client_credentials, api_key, basic", t)
		}
	}

	// Which OAuth2 grant to use.
	//
	// The field was stored and read by nothing: which grant ran was decided by
	// the auth type alone, so `grant_type = "password"` — a grant this does not
	// implement — was accepted in silence and a refresh-token flow ran instead,
	// against a token endpoint that would refuse it. Two are implemented, and
	// anything else is named rather than ignored.
	if grantType, ok := cfg["grant_type"].(string); ok && grantType != "" {
		switch grantType {
		case "client_credentials":
			auth.Type = AuthTypeClientCredentials
		case "refresh_token":
			auth.Type = AuthTypeOAuth2
		default:
			return nil, fmt.Errorf(
				"auth grant_type %q is not one this implements; use client_credentials or refresh_token", grantType)
		}
		auth.GrantType = grantType
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

	return auth, nil
}

// parseTLSConfig parses TLS configuration from HCL.
//
// The names are the canonical ones; the parser folds client_cert and
// client_key, which this connector used to read directly, onto cert and key.
func parseTLSConfig(cfg map[string]interface{}) *TLSConfig {
	tls := &TLSConfig{}

	if caCert, ok := cfg["ca_cert"].(string); ok {
		tls.CACert = caCert
	}
	if clientCert, ok := cfg["cert"].(string); ok {
		tls.ClientCert = clientCert
	}
	if clientKey, ok := cfg["key"].(string); ok {
		tls.ClientKey = clientKey
	}
	if insecure, ok := connector.BoolFromPropsStrict(cfg, "insecure_skip_verify"); ok {
		tls.InsecureSkipVerify = insecure
	}

	return tls
}

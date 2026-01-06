package rest

import (
	"context"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates REST connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new REST connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "rest"
}

// Create creates a new REST connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	port := cfg.GetInt("port")
	if port == 0 {
		port = 3000 // default port
	}

	var cors *CORSConfig
	if corsMap := cfg.GetMap("cors"); corsMap != nil {
		cors = &CORSConfig{}

		if origins, ok := corsMap["origins"].([]interface{}); ok {
			for _, o := range origins {
				if s, ok := o.(string); ok {
					cors.Origins = append(cors.Origins, s)
				}
			}
		}

		if methods, ok := corsMap["methods"].([]interface{}); ok {
			for _, m := range methods {
				if s, ok := m.(string); ok {
					cors.Methods = append(cors.Methods, s)
				}
			}
		}

		if headers, ok := corsMap["headers"].([]interface{}); ok {
			for _, h := range headers {
				if s, ok := h.(string); ok {
					cors.Headers = append(cors.Headers, s)
				}
			}
		}
	}

	// Create connector
	conn := New(cfg.Name, port, cors, f.logger)

	// Configure authentication if present
	if authMap := cfg.GetMap("auth"); authMap != nil {
		authConfig := parseAuthConfig(authMap)
		conn.SetAuthConfig(authConfig)
	}

	return conn, nil
}

// parseAuthConfig parses auth configuration from HCL map.
func parseAuthConfig(authMap map[string]interface{}) *AuthConfig {
	cfg := &AuthConfig{}

	// Get auth type
	if t, ok := authMap["type"].(string); ok {
		cfg.Type = t
	}

	// Get public paths
	if public, ok := authMap["public"].([]interface{}); ok {
		for _, p := range public {
			if s, ok := p.(string); ok {
				cfg.Public = append(cfg.Public, s)
			}
		}
	}

	// Get required headers
	if headers, ok := authMap["required_headers"].([]interface{}); ok {
		for _, h := range headers {
			if s, ok := h.(string); ok {
				cfg.RequiredHeaders = append(cfg.RequiredHeaders, s)
			}
		}
	}

	// Get response headers
	if respHeaders, ok := authMap["response_headers"].(map[string]interface{}); ok {
		cfg.ResponseHeaders = make(map[string]string)
		for k, v := range respHeaders {
			if s, ok := v.(string); ok {
				cfg.ResponseHeaders[k] = s
			}
		}
	}

	// Parse JWT config
	if cfg.Type == "jwt" {
		cfg.JWT = parseJWTConfig(authMap)
	}

	// Parse API Key config
	if cfg.Type == "api_key" {
		cfg.APIKey = parseAPIKeyConfig(authMap)
	}

	// Parse Basic config
	if cfg.Type == "basic" {
		cfg.Basic = parseBasicConfig(authMap)
	}

	return cfg
}

// parseJWTConfig parses JWT authentication configuration.
func parseJWTConfig(authMap map[string]interface{}) *JWTAuthConfig {
	cfg := &JWTAuthConfig{
		ClockSkew: 5 * time.Second,
	}

	if secret, ok := authMap["secret"].(string); ok {
		cfg.Secret = secret
	}
	if jwksURL, ok := authMap["jwks_url"].(string); ok {
		cfg.JWKSURL = jwksURL
	}
	if issuer, ok := authMap["issuer"].(string); ok {
		cfg.Issuer = issuer
	}
	if header, ok := authMap["header"].(string); ok {
		cfg.Header = header
	}
	if scheme, ok := authMap["scheme"].(string); ok {
		cfg.Scheme = scheme
	}

	// Audience can be string or array
	if aud, ok := authMap["audience"].(string); ok {
		cfg.Audience = []string{aud}
	} else if aud, ok := authMap["audience"].([]interface{}); ok {
		for _, a := range aud {
			if s, ok := a.(string); ok {
				cfg.Audience = append(cfg.Audience, s)
			}
		}
	}

	// Algorithms
	if algs, ok := authMap["algorithms"].([]interface{}); ok {
		for _, a := range algs {
			if s, ok := a.(string); ok {
				cfg.Algorithms = append(cfg.Algorithms, s)
			}
		}
	}

	return cfg
}

// parseAPIKeyConfig parses API Key authentication configuration.
func parseAPIKeyConfig(authMap map[string]interface{}) *APIKeyAuthConfig {
	cfg := &APIKeyAuthConfig{}

	if header, ok := authMap["header"].(string); ok {
		cfg.Header = header
	}
	if query, ok := authMap["query_param"].(string); ok {
		cfg.QueryParam = query
	}

	// Keys can be single string or array
	if key, ok := authMap["keys"].(string); ok {
		cfg.Keys = []string{key}
	} else if keys, ok := authMap["keys"].([]interface{}); ok {
		for _, k := range keys {
			if s, ok := k.(string); ok {
				cfg.Keys = append(cfg.Keys, s)
			}
		}
	}

	return cfg
}

// parseBasicConfig parses Basic authentication configuration.
func parseBasicConfig(authMap map[string]interface{}) *BasicAuthConfig {
	cfg := &BasicAuthConfig{
		Users: make(map[string]string),
	}

	if realm, ok := authMap["realm"].(string); ok {
		cfg.Realm = realm
	}

	// Users map
	if users, ok := authMap["users"].(map[string]interface{}); ok {
		for username, password := range users {
			if pwd, ok := password.(string); ok {
				cfg.Users[username] = pwd
			}
		}
	}

	return cfg
}

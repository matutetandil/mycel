package soap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates SOAP connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new SOAP connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{logger: logger}
}

// Supports returns true for "soap" connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "soap"
}

// Create creates a SOAP client or server depending on configuration.
// - endpoint set → Client (call external SOAP services)
// - port set → Server (expose SOAP endpoints)
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	endpoint, _ := cfg.Properties["endpoint"].(string)
	port := cfg.GetInt("port")

	if endpoint != "" && port != 0 {
		return nil, fmt.Errorf("soap connector %s: set either 'endpoint' (client) or 'port' (server), not both", cfg.Name)
	}
	if endpoint == "" && port == 0 {
		return nil, fmt.Errorf("soap connector %s: requires either 'endpoint' (client) or 'port' (server)", cfg.Name)
	}

	soapVersion, _ := cfg.Properties["soap_version"].(string)
	if soapVersion == "" {
		soapVersion = "1.1"
	}
	namespace, _ := cfg.Properties["namespace"].(string)

	if endpoint != "" {
		return f.createClient(cfg, endpoint, soapVersion, namespace)
	}
	return f.createServer(cfg, port, soapVersion, namespace)
}

func (f *Factory) createClient(cfg *connector.Config, endpoint, soapVersion, namespace string) (*Client, error) {
	timeout := 30 * time.Second
	if t, ok := cfg.Properties["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(t); err == nil {
			timeout = parsed
		}
	}

	// Parse auth
	var auth *AuthConfig
	if authMap, ok := cfg.Properties["auth"].(map[string]interface{}); ok {
		auth = &AuthConfig{}
		if t, ok := authMap["type"].(string); ok {
			auth.Type = t
		}
		if u, ok := authMap["username"].(string); ok {
			auth.Username = u
		}
		if p, ok := authMap["password"].(string); ok {
			auth.Password = p
		}
		if t, ok := authMap["token"].(string); ok {
			auth.Token = t
		}
	}

	// Parse custom headers
	headers := make(map[string]string)
	if h, ok := cfg.Properties["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	return NewClient(cfg.Name, endpoint, soapVersion, namespace, timeout, auth, headers), nil
}

func (f *Factory) createServer(cfg *connector.Config, port int, soapVersion, namespace string) (*Server, error) {
	return NewServer(cfg.Name, port, soapVersion, namespace, f.logger), nil
}

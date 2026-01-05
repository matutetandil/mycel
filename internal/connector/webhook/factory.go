package webhook

import (
	"fmt"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates webhook connectors
type Factory struct{}

// NewFactory creates a new webhook factory
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles
func (f *Factory) Type() string {
	return "webhook"
}

// Create creates a new webhook connector from configuration
func (f *Factory) Create(name string, config map[string]interface{}) (connector.Connector, error) {
	mode := getString(config, "mode", "outbound")

	switch mode {
	case "inbound", "receive", "server":
		return f.createInbound(name, config)
	case "outbound", "send", "client":
		return f.createOutbound(name, config)
	default:
		return nil, fmt.Errorf("unknown webhook mode: %s (use 'inbound' or 'outbound')", mode)
	}
}

func (f *Factory) createInbound(name string, config map[string]interface{}) (connector.Connector, error) {
	cfg := DefaultInboundConfig()

	if path := getString(config, "path", ""); path != "" {
		cfg.Path = path
	}
	if secret := getString(config, "secret", ""); secret != "" {
		cfg.Secret = secret
	}
	if header := getString(config, "signature_header", ""); header != "" {
		cfg.SignatureHeader = header
	}
	if algo := getString(config, "signature_algorithm", ""); algo != "" {
		cfg.SignatureAlgorithm = algo
	}
	if header := getString(config, "timestamp_header", ""); header != "" {
		cfg.TimestampHeader = header
	}
	if tolerance := getString(config, "timestamp_tolerance", ""); tolerance != "" {
		if d, err := time.ParseDuration(tolerance); err == nil {
			cfg.TimestampTolerance = d
		}
	}
	if ips := getStringSlice(config, "allowed_ips"); len(ips) > 0 {
		cfg.AllowedIPs = ips
	}
	if requireHTTPS, ok := config["require_https"].(bool); ok {
		cfg.RequireHTTPS = requireHTTPS
	}

	return NewInboundConnector(name, cfg), nil
}

func (f *Factory) createOutbound(name string, config map[string]interface{}) (connector.Connector, error) {
	cfg := DefaultOutboundConfig()

	if url := getString(config, "url", ""); url != "" {
		cfg.URL = url
	}
	if method := getString(config, "method", ""); method != "" {
		cfg.Method = method
	}
	if secret := getString(config, "secret", ""); secret != "" {
		cfg.Secret = secret
	}
	if header := getString(config, "signature_header", ""); header != "" {
		cfg.SignatureHeader = header
	}
	if algo := getString(config, "signature_algorithm", ""); algo != "" {
		cfg.SignatureAlgorithm = algo
	}
	if includeTs, ok := config["include_timestamp"].(bool); ok {
		cfg.IncludeTimestamp = includeTs
	}
	if timeout := getString(config, "timeout", ""); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.Timeout = d
		}
	}

	// Headers
	if headers, ok := config["headers"].(map[string]interface{}); ok {
		cfg.Headers = make(map[string]string)
		for k, v := range headers {
			if s, ok := v.(string); ok {
				cfg.Headers[k] = s
			}
		}
	}

	// Retry configuration
	if retry, ok := config["retry"].(map[string]interface{}); ok {
		if attempts := getInt(retry, "max_attempts", 0); attempts > 0 {
			cfg.Retry.MaxAttempts = attempts
		}
		if delay := getString(retry, "initial_delay", ""); delay != "" {
			if d, err := time.ParseDuration(delay); err == nil {
				cfg.Retry.InitialDelay = d
			}
		}
		if maxDelay := getString(retry, "max_delay", ""); maxDelay != "" {
			if d, err := time.ParseDuration(maxDelay); err == nil {
				cfg.Retry.MaxDelay = d
			}
		}
		if multiplier, ok := retry["multiplier"].(float64); ok {
			cfg.Retry.Multiplier = multiplier
		}
		if statuses := getIntSlice(retry, "retryable_statuses"); len(statuses) > 0 {
			cfg.Retry.RetryableStatuses = statuses
		}
	}

	return NewOutboundConnector(name, cfg), nil
}

// Helper functions
func getString(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

func getInt(m map[string]interface{}, key string, defaultVal int) int {
	if v, ok := m[key].(int); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	if v, ok := m[key].([]string); ok {
		return v
	}
	return nil
}

func getIntSlice(m map[string]interface{}, key string) []int {
	if v, ok := m[key].([]interface{}); ok {
		result := make([]int, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case int:
				result = append(result, n)
			case float64:
				result = append(result, int(n))
			}
		}
		return result
	}
	return nil
}

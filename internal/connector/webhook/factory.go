package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Factory creates webhook connectors
type Factory struct{}

// NewFactory creates a new webhook factory
func NewFactory() *Factory {
	return &Factory{}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connectorType, driver string) bool {
	return connectorType == "webhook"
}

// Create creates a new webhook connector from configuration
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	props := config.Properties
	mode := getString(props, "mode", "outbound")

	switch mode {
	case "inbound", "receive", "server":
		return f.createInbound(config.Name, props)
	case "outbound", "send", "client":
		return f.createOutbound(config.Name, props)
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
	if proxies := getStringSlice(config, "trusted_proxies"); len(proxies) > 0 {
		cfg.TrustedProxies = proxies
	}
	// Written as a word when it comes from env(), and this one decides whether
	// a webhook is accepted over plaintext.
	cfg.RequireHTTPS = connector.BoolFromProps(config, "require_https", cfg.RequireHTTPS)

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
	cfg.IncludeTimestamp = connector.BoolFromProps(config, "include_timestamp", cfg.IncludeTimestamp)
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

	// Retry configuration. The names are the canonical ones; the parser folds
	// max_attempts and initial_delay, which this connector used to read
	// directly, onto attempts and delay. Neither was accepted by the parser
	// before that, so nothing ever reached these fields.
	if retry, ok := config["retry"].(map[string]interface{}); ok {
		if attempts := getInt(retry, "attempts", 0); attempts > 0 {
			cfg.Retry.MaxAttempts = attempts
		}
		if delay := getString(retry, "delay", ""); delay != "" {
			if d, err := time.ParseDuration(delay); err == nil {
				cfg.Retry.InitialDelay = d
			}
		}
		if maxDelay := getString(retry, "max_delay", ""); maxDelay != "" {
			if d, err := time.ParseDuration(maxDelay); err == nil {
				cfg.Retry.MaxDelay = d
			}
		}
		// A whole number written in HCL arrives as an int, so multiplier = 2.0
		// would have failed a float64 type assertion and been dropped.
		if multiplier := getFloat(retry, "multiplier", 0); multiplier > 0 {
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
	return connector.IntFromProps(m, key, defaultVal)
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

// getFloat reads a number that may have arrived as either an int or a float,
// which is what HCL produces depending on whether the literal was whole.
func getFloat(props map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := props[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return defaultVal
}

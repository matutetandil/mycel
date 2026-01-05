package email

import (
	"context"
	"fmt"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// EmailConnector is the unified email connector interface
type EmailConnector interface {
	connector.Connector
	Send(ctx context.Context, email *Email) (*SendResult, error)
}

// Factory creates email connectors
type Factory struct{}

// NewFactory creates a new email factory
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles
func (f *Factory) Type() string {
	return "email"
}

// Create creates a new email connector from configuration
func (f *Factory) Create(name string, config map[string]interface{}) (connector.Connector, error) {
	driver := getString(config, "driver", "smtp")

	cfg := &Config{
		Name:     name,
		Driver:   driver,
		From:     getString(config, "from", ""),
		FromName: getString(config, "from_name", ""),
		ReplyTo:  getString(config, "reply_to", ""),
	}

	switch driver {
	case "smtp":
		return f.createSMTP(name, cfg, config)
	case "sendgrid":
		return f.createSendGrid(name, cfg, config)
	case "ses":
		return f.createSES(name, cfg, config)
	default:
		return nil, fmt.Errorf("unknown email driver: %s (use 'smtp', 'sendgrid', or 'ses')", driver)
	}
}

func (f *Factory) createSMTP(name string, cfg *Config, config map[string]interface{}) (connector.Connector, error) {
	cfg.SMTP = &SMTPConfig{
		Host:     getString(config, "host", "localhost"),
		Port:     getInt(config, "port", 587),
		Username: getString(config, "username", ""),
		Password: getString(config, "password", ""),
		TLS:      getString(config, "tls", "starttls"),
		Timeout:  getDuration(config, "timeout", 30*time.Second),
		PoolSize: getInt(config, "pool_size", 5),
	}

	return NewSMTPConnector(name, cfg), nil
}

func (f *Factory) createSendGrid(name string, cfg *Config, config map[string]interface{}) (connector.Connector, error) {
	apiKey := getString(config, "api_key", "")
	if apiKey == "" {
		return nil, fmt.Errorf("SendGrid api_key is required")
	}

	cfg.SendGrid = &SendGridConfig{
		APIKey:   apiKey,
		Endpoint: getString(config, "endpoint", "https://api.sendgrid.com"),
		Timeout:  getDuration(config, "timeout", 30*time.Second),
	}

	return NewSendGridConnector(name, cfg), nil
}

func (f *Factory) createSES(name string, cfg *Config, config map[string]interface{}) (connector.Connector, error) {
	cfg.SES = &SESConfig{
		Region:           getString(config, "region", "us-east-1"),
		AccessKeyID:      getString(config, "access_key_id", ""),
		SecretAccessKey:  getString(config, "secret_access_key", ""),
		ConfigurationSet: getString(config, "configuration_set", ""),
		Timeout:          getDuration(config, "timeout", 30*time.Second),
	}

	return NewSESConnector(name, cfg), nil
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

func getDuration(m map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if v, ok := m[key].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}

package mq

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/mq/kafka"
	"github.com/matutetandil/mycel/internal/connector/mq/rabbitmq"
)

// Factory creates message queue connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new MQ connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the specified connector type.
func (f *Factory) Supports(connType, driver string) bool {
	if connType != "mq" {
		return false
	}
	switch driver {
	case "rabbitmq", "":
		return true
	case "kafka":
		return true
	// Future: "sqs", "nats"
	default:
		return false
	}
}

// Create creates a new MQ connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = "rabbitmq" // Default driver
	}

	switch driver {
	case "rabbitmq":
		return f.createRabbitMQ(cfg)
	case "kafka":
		return f.createKafka(cfg)
	default:
		return nil, fmt.Errorf("unsupported MQ driver: %s", driver)
	}
}

// createRabbitMQ creates a RabbitMQ connector from configuration.
func (f *Factory) createRabbitMQ(cfg *connector.Config) (*rabbitmq.Connector, error) {
	config := rabbitmq.DefaultConfig()

	// Connection settings
	config.Host = getString(cfg.Properties, "host", "localhost")
	config.Port = getInt(cfg.Properties, "port", 5672)
	config.Username = getString(cfg.Properties, "username", "guest")
	config.Password = getString(cfg.Properties, "password", "guest")
	config.Vhost = getString(cfg.Properties, "vhost", "/")

	// Connection options
	config.Heartbeat = getDuration(cfg.Properties, "heartbeat", 10*time.Second)
	config.ConnectionName = getString(cfg.Properties, "connection_name", cfg.Name)
	config.ReconnectDelay = getDuration(cfg.Properties, "reconnect_delay", 5*time.Second)
	config.MaxReconnects = getInt(cfg.Properties, "max_reconnects", 10)

	// TLS configuration
	if tlsCfg := getMap(cfg.Properties, "tls"); tlsCfg != nil {
		if getBool(tlsCfg, "enabled", false) {
			config.TLS = &rabbitmq.TLSConfig{
				Enabled:            true,
				CertFile:           getString(tlsCfg, "cert", ""),
				KeyFile:            getString(tlsCfg, "key", ""),
				CAFile:             getString(tlsCfg, "ca_cert", ""),
				InsecureSkipVerify: getBool(tlsCfg, "insecure_skip_verify", false),
			}
		}
	}

	// Queue configuration
	if queueCfg := getMap(cfg.Properties, "queue"); queueCfg != nil {
		config.Queue = &rabbitmq.QueueConfig{
			Name:       getString(queueCfg, "name", ""),
			Durable:    getBool(queueCfg, "durable", true),
			AutoDelete: getBool(queueCfg, "auto_delete", false),
			Exclusive:  getBool(queueCfg, "exclusive", false),
			NoWait:     getBool(queueCfg, "no_wait", false),
			Args:       getMap(queueCfg, "args"),
		}
	}

	// Exchange configuration
	if exchangeCfg := getMap(cfg.Properties, "exchange"); exchangeCfg != nil {
		exchangeType := getString(exchangeCfg, "type", "direct")
		config.Exchange = &rabbitmq.ExchangeConfig{
			Name:       getString(exchangeCfg, "name", ""),
			Type:       rabbitmq.ExchangeType(exchangeType),
			Durable:    getBool(exchangeCfg, "durable", true),
			AutoDelete: getBool(exchangeCfg, "auto_delete", false),
			Internal:   getBool(exchangeCfg, "internal", false),
			NoWait:     getBool(exchangeCfg, "no_wait", false),
			RoutingKey: getString(exchangeCfg, "routing_key", ""),
			Args:       getMap(exchangeCfg, "args"),
			BindArgs:   getMap(exchangeCfg, "bind_args"),
		}
	}

	// Consumer configuration
	if consumerCfg := getMap(cfg.Properties, "consumer"); consumerCfg != nil {
		config.Consumer = &rabbitmq.ConsumerConfig{
			Tag:         getString(consumerCfg, "tag", ""),
			AutoAck:     getBool(consumerCfg, "auto_ack", false),
			Exclusive:   getBool(consumerCfg, "exclusive", false),
			NoLocal:     getBool(consumerCfg, "no_local", false),
			NoWait:      getBool(consumerCfg, "no_wait", false),
			Concurrency: getInt(consumerCfg, "concurrency", 1),
			Prefetch:    getInt(consumerCfg, "prefetch", 10),
			Args:        getMap(consumerCfg, "args"),
		}
	}

	// Publisher configuration
	if publisherCfg := getMap(cfg.Properties, "publisher"); publisherCfg != nil {
		config.Publisher = &rabbitmq.PublisherConfig{
			Exchange:    getString(publisherCfg, "exchange", ""),
			RoutingKey:  getString(publisherCfg, "routing_key", ""),
			Mandatory:   getBool(publisherCfg, "mandatory", false),
			Immediate:   getBool(publisherCfg, "immediate", false),
			Persistent:  getBool(publisherCfg, "persistent", true),
			ContentType: getString(publisherCfg, "content_type", "application/json"),
			Confirms:    getBool(publisherCfg, "confirms", false),
		}
	}

	return rabbitmq.NewConnector(cfg.Name, config, f.logger)
}

// Helper functions for extracting configuration values

func getString(props map[string]interface{}, key, defaultVal string) string {
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getInt(props map[string]interface{}, key string, defaultVal int) int {
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return defaultVal
}

func getBool(props map[string]interface{}, key string, defaultVal bool) bool {
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getDuration(props map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		switch d := v.(type) {
		case string:
			if parsed, err := time.ParseDuration(d); err == nil {
				return parsed
			}
		case time.Duration:
			return d
		case int64:
			return time.Duration(d) * time.Millisecond
		case float64:
			return time.Duration(d) * time.Millisecond
		}
	}
	return defaultVal
}

func getMap(props map[string]interface{}, key string) map[string]interface{} {
	if props == nil {
		return nil
	}
	if v, ok := props[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

func getStringSlice(props map[string]interface{}, key string, defaultVal []string) []string {
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		switch s := v.(type) {
		case []string:
			return s
		case []interface{}:
			result := make([]string, 0, len(s))
			for _, item := range s {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}
	return defaultVal
}

// createKafka creates a Kafka connector from configuration.
func (f *Factory) createKafka(cfg *connector.Config) (*kafka.Connector, error) {
	config := kafka.DefaultConfig()

	// Broker addresses
	config.Brokers = getStringSlice(cfg.Properties, "brokers", []string{"localhost:9092"})
	config.ClientID = getString(cfg.Properties, "client_id", cfg.Name)

	// TLS configuration
	if tlsCfg := getMap(cfg.Properties, "tls"); tlsCfg != nil {
		if getBool(tlsCfg, "enabled", false) {
			config.TLS = &kafka.TLSConfig{
				Enabled:            true,
				CertFile:           getString(tlsCfg, "cert", ""),
				KeyFile:            getString(tlsCfg, "key", ""),
				CAFile:             getString(tlsCfg, "ca_cert", ""),
				InsecureSkipVerify: getBool(tlsCfg, "insecure_skip_verify", false),
			}
		}
	}

	// SASL configuration
	if saslCfg := getMap(cfg.Properties, "sasl"); saslCfg != nil {
		config.SASL = &kafka.SASLConfig{
			Mechanism: getString(saslCfg, "mechanism", "PLAIN"),
			Username:  getString(saslCfg, "username", ""),
			Password:  getString(saslCfg, "password", ""),
		}
	}

	// Consumer configuration
	if consumerCfg := getMap(cfg.Properties, "consumer"); consumerCfg != nil {
		config.Consumer = &kafka.ConsumerConfig{
			GroupID:         getString(consumerCfg, "group_id", ""),
			Topics:          getStringSlice(consumerCfg, "topics", nil),
			AutoOffsetReset: getString(consumerCfg, "auto_offset_reset", "earliest"),
			AutoCommit:      getBool(consumerCfg, "auto_commit", true),
			MinBytes:        getInt(consumerCfg, "min_bytes", 1),
			MaxBytes:        getInt(consumerCfg, "max_bytes", 10*1024*1024),
			MaxWaitTime:     getDuration(consumerCfg, "max_wait_time", 500*time.Millisecond),
			Concurrency:     getInt(consumerCfg, "concurrency", 1),
		}
	}

	// Producer configuration
	if producerCfg := getMap(cfg.Properties, "producer"); producerCfg != nil {
		config.Producer = &kafka.ProducerConfig{
			Topic:       getString(producerCfg, "topic", ""),
			Acks:        getString(producerCfg, "acks", "all"),
			Retries:     getInt(producerCfg, "retries", 3),
			BatchSize:   getInt(producerCfg, "batch_size", 16384),
			LingerMs:    getInt(producerCfg, "linger_ms", 5),
			Compression: getString(producerCfg, "compression", "none"),
		}
	}

	return kafka.NewConnector(cfg.Name, config, f.logger)
}

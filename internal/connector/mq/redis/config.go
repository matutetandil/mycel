// Package redis provides a Redis Pub/Sub connector for the Mycel MQ system.
package redis

import "fmt"

// Config holds the Redis Pub/Sub connector configuration.
type Config struct {
	// Connection settings
	Host     string
	Port     int
	Password string
	DB       int

	// Pub/Sub channels to subscribe to (exact match)
	Channels []string

	// Pub/Sub patterns to psubscribe to (glob-style)
	Patterns []string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Host: "localhost",
		Port: 6379,
		DB:   0,
	}
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	if c.DB < 0 {
		return fmt.Errorf("invalid db: %d", c.DB)
	}
	return nil
}

// IsSubscriber returns true if this config has channels or patterns to subscribe to.
func (c *Config) IsSubscriber() bool {
	return len(c.Channels) > 0 || len(c.Patterns) > 0
}

// Addr returns the Redis address in host:port format.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Package logging provides centralized logging configuration for Mycel.
// It supports configuration via environment variables:
//   - MYCEL_LOG_LEVEL: debug, info, warn, error (default: info)
//   - MYCEL_LOG_FORMAT: text, json (default: text)
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Environment variable names
const (
	EnvLogLevel  = "MYCEL_LOG_LEVEL"
	EnvLogFormat = "MYCEL_LOG_FORMAT"
)

// Config holds the logging configuration.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string

	// Format is the output format (text, json).
	Format string

	// Output is where logs are written. Defaults to os.Stdout.
	Output io.Writer
}

// DefaultConfig returns the default logging configuration.
func DefaultConfig() *Config {
	return &Config{
		Level:  "info",
		Format: "text",
		Output: os.Stdout,
	}
}

// ConfigFromEnv creates a Config from environment variables.
// It uses defaults for any unset variables.
func ConfigFromEnv() *Config {
	cfg := DefaultConfig()

	if level := os.Getenv(EnvLogLevel); level != "" {
		cfg.Level = strings.ToLower(level)
	}

	if format := os.Getenv(EnvLogFormat); format != "" {
		cfg.Format = strings.ToLower(format)
	}

	return cfg
}

// NewLogger creates a new slog.Logger based on the configuration.
func NewLogger(cfg *Config) *slog.Logger {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}

	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(cfg.Output, opts)
	default:
		handler = slog.NewTextHandler(cfg.Output, opts)
	}

	return slog.New(handler)
}

// NewLoggerFromEnv creates a logger configured from environment variables.
func NewLoggerFromEnv() *slog.Logger {
	return NewLogger(ConfigFromEnv())
}

// parseLevel converts a string level to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseLevel is exported for use by CLI to validate levels.
func ParseLevel(level string) slog.Level {
	return parseLevel(level)
}

// ValidLevels returns the list of valid log levels.
func ValidLevels() []string {
	return []string{"debug", "info", "warn", "error"}
}

// ValidFormats returns the list of valid log formats.
func ValidFormats() []string {
	return []string{"text", "json"}
}

// IsValidLevel checks if a level string is valid.
func IsValidLevel(level string) bool {
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "warning", "error":
		return true
	}
	return false
}

// IsValidFormat checks if a format string is valid.
func IsValidFormat(format string) bool {
	switch strings.ToLower(format) {
	case "text", "json":
		return true
	}
	return false
}

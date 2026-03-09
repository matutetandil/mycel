package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("expected level 'info', got '%s'", cfg.Level)
	}
	if cfg.Format != "text" {
		t.Errorf("expected format 'text', got '%s'", cfg.Format)
	}
	if cfg.Output != os.Stdout {
		t.Error("expected output to be os.Stdout")
	}
}

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name           string
		envLevel       string
		envFormat      string
		expectedLevel  string
		expectedFormat string
	}{
		{
			name:           "defaults when no env vars",
			expectedLevel:  "info",
			expectedFormat: "text",
		},
		{
			name:           "debug level from env",
			envLevel:       "debug",
			expectedLevel:  "debug",
			expectedFormat: "text",
		},
		{
			name:           "json format from env",
			envFormat:      "json",
			expectedLevel:  "info",
			expectedFormat: "json",
		},
		{
			name:           "both from env",
			envLevel:       "error",
			envFormat:      "json",
			expectedLevel:  "error",
			expectedFormat: "json",
		},
		{
			name:           "case insensitive",
			envLevel:       "DEBUG",
			envFormat:      "JSON",
			expectedLevel:  "debug",
			expectedFormat: "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore env vars
			oldLevel := os.Getenv(EnvLogLevel)
			oldFormat := os.Getenv(EnvLogFormat)
			defer func() {
				os.Setenv(EnvLogLevel, oldLevel)
				os.Setenv(EnvLogFormat, oldFormat)
			}()

			// Set test values
			if tt.envLevel != "" {
				os.Setenv(EnvLogLevel, tt.envLevel)
			} else {
				os.Unsetenv(EnvLogLevel)
			}
			if tt.envFormat != "" {
				os.Setenv(EnvLogFormat, tt.envFormat)
			} else {
				os.Unsetenv(EnvLogFormat)
			}

			cfg := ConfigFromEnv()

			if cfg.Level != tt.expectedLevel {
				t.Errorf("expected level '%s', got '%s'", tt.expectedLevel, cfg.Level)
			}
			if cfg.Format != tt.expectedFormat {
				t.Errorf("expected format '%s', got '%s'", tt.expectedFormat, cfg.Format)
			}
		})
	}
}

func TestNewLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: &buf,
	}

	logger := NewLogger(cfg)
	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected log to contain 'test message', got: %s", output)
	}
	// tint includes ANSI color codes, so check for key and value separately
	if !strings.Contains(output, "key=") || !strings.Contains(output, "value") {
		t.Errorf("expected log to contain key=value, got: %s", output)
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := &Config{
		Level:  "info",
		Format: "json",
		Output: &buf,
	}

	logger := NewLogger(cfg)
	logger.Info("test message", "key", "value")

	// Parse JSON output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if msg, ok := logEntry["msg"].(string); !ok || msg != "test message" {
		t.Errorf("expected msg 'test message', got: %v", logEntry["msg"])
	}
	if val, ok := logEntry["key"].(string); !ok || val != "value" {
		t.Errorf("expected key 'value', got: %v", logEntry["key"])
	}
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		configLevel   string
		logLevel      slog.Level
		shouldAppear  bool
	}{
		{"debug", slog.LevelDebug, true},
		{"debug", slog.LevelInfo, true},
		{"info", slog.LevelDebug, false},
		{"info", slog.LevelInfo, true},
		{"warn", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelWarn, false},
		{"error", slog.LevelError, true},
	}

	for _, tt := range tests {
		t.Run(tt.configLevel+"_"+tt.logLevel.String(), func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &Config{
				Level:  tt.configLevel,
				Format: "text",
				Output: &buf,
			}

			logger := NewLogger(cfg)
			logger.Log(nil, tt.logLevel, "test")

			hasOutput := buf.Len() > 0
			if hasOutput != tt.shouldAppear {
				t.Errorf("config level %s, log level %s: expected appear=%v, got appear=%v",
					tt.configLevel, tt.logLevel, tt.shouldAppear, hasOutput)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelInfo}, // defaults to info
		{"", slog.LevelInfo},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidLevel(t *testing.T) {
	valid := []string{"debug", "DEBUG", "info", "INFO", "warn", "WARN", "warning", "error", "ERROR"}
	for _, level := range valid {
		if !IsValidLevel(level) {
			t.Errorf("IsValidLevel(%q) should be true", level)
		}
	}

	invalid := []string{"trace", "fatal", "invalid", ""}
	for _, level := range invalid {
		if IsValidLevel(level) {
			t.Errorf("IsValidLevel(%q) should be false", level)
		}
	}
}

func TestIsValidFormat(t *testing.T) {
	valid := []string{"text", "TEXT", "json", "JSON"}
	for _, format := range valid {
		if !IsValidFormat(format) {
			t.Errorf("IsValidFormat(%q) should be true", format)
		}
	}

	invalid := []string{"xml", "yaml", "invalid", ""}
	for _, format := range invalid {
		if IsValidFormat(format) {
			t.Errorf("IsValidFormat(%q) should be false", format)
		}
	}
}

func TestNewLogger_NilConfig(t *testing.T) {
	// Should not panic and return a valid logger
	logger := NewLogger(nil)
	if logger == nil {
		t.Error("NewLogger(nil) should return a valid logger")
	}
}

func TestNewLoggerFromEnv(t *testing.T) {
	// Save and restore env vars
	oldLevel := os.Getenv(EnvLogLevel)
	oldFormat := os.Getenv(EnvLogFormat)
	defer func() {
		os.Setenv(EnvLogLevel, oldLevel)
		os.Setenv(EnvLogFormat, oldFormat)
	}()

	os.Setenv(EnvLogLevel, "debug")
	os.Setenv(EnvLogFormat, "json")

	logger := NewLoggerFromEnv()
	if logger == nil {
		t.Error("NewLoggerFromEnv() should return a valid logger")
	}
}

package logging

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Payload logging environment variables.
const (
	// EnvPayloadShow opts in to logging the incoming payload of every flow.
	// It is independent of MYCEL_LOG_LEVEL, but payloads are only emitted at
	// debug level, so both must be set to actually see them. Kept off by
	// default because payloads may contain PII or secrets.
	EnvPayloadShow = "MYCEL_PAYLOAD_SHOW"

	// EnvPayloadSize caps how many bytes of the (JSON-encoded) payload are
	// logged. Accepts a plain byte count or a size with a k/m suffix
	// (e.g. "512", "4k", "1m"). Defaults to DefaultPayloadMaxBytes.
	EnvPayloadSize = "MYCEL_PAYLOAD_SIZE"
)

// DefaultPayloadMaxBytes is the truncation cap used when MYCEL_PAYLOAD_SIZE
// is unset or unparseable.
const DefaultPayloadMaxBytes = 4096

// PayloadLogConfig controls debug logging of incoming flow payloads.
type PayloadLogConfig struct {
	// Show enables logging the incoming payload. Off by default.
	Show bool

	// MaxBytes is the truncation cap for the logged payload.
	MaxBytes int
}

// PayloadLogFromEnv builds a PayloadLogConfig from environment variables,
// falling back to safe defaults (disabled, 4k cap).
func PayloadLogFromEnv() PayloadLogConfig {
	cfg := PayloadLogConfig{
		Show:     false,
		MaxBytes: DefaultPayloadMaxBytes,
	}

	if v := os.Getenv(EnvPayloadShow); v != "" {
		cfg.Show = strings.EqualFold(v, "true") || v == "1"
	}

	if v := os.Getenv(EnvPayloadSize); v != "" {
		if n, err := ParseSize(v); err == nil && n > 0 {
			cfg.MaxBytes = n
		}
	}

	return cfg
}

// ParseSize parses a human-friendly byte size. It accepts a plain integer
// (bytes) or an integer with a single k/m suffix (case-insensitive, decimal
// units: 1k = 1024, 1m = 1024*1024). A "b" suffix is treated as bytes.
func ParseSize(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	mult := 1
	switch s[len(s)-1] {
	case 'k':
		mult = 1024
		s = s[:len(s)-1]
	case 'm':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case 'b':
		s = s[:len(s)-1]
	}

	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative size %q", s)
	}
	return n * mult, nil
}

// Package sanitize provides the core input sanitization pipeline for Mycel.
// The pipeline always runs before any flow execution and cannot be disabled.
// It protects against common attack vectors: null bytes, control characters,
// oversized inputs, deep nesting, and invalid UTF-8.
package sanitize

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxInputLength is the maximum total input size (1MB).
	DefaultMaxInputLength = 1 * 1024 * 1024

	// DefaultMaxFieldLength is the maximum length of a single string field (64KB).
	DefaultMaxFieldLength = 64 * 1024

	// DefaultMaxFieldDepth is the maximum nesting depth.
	DefaultMaxFieldDepth = 20
)

// Config holds adjustable thresholds for sanitization.
// These can be overridden via HCL security blocks but the core pipeline always runs.
type Config struct {
	// MaxInputLength is the maximum total input size in bytes.
	MaxInputLength int

	// MaxFieldLength is the maximum length of a single string field in bytes.
	MaxFieldLength int

	// MaxFieldDepth is the maximum nesting depth for input data.
	MaxFieldDepth int

	// AllowedControlChars is the set of permitted control characters.
	// Default: tab, newline, carriage return.
	AllowedControlChars map[byte]bool
}

// DefaultConfig returns secure defaults.
func DefaultConfig() *Config {
	return &Config{
		MaxInputLength: DefaultMaxInputLength,
		MaxFieldLength: DefaultMaxFieldLength,
		MaxFieldDepth:  DefaultMaxFieldDepth,
		AllowedControlChars: map[byte]bool{
			'\t': true, // tab
			'\n': true, // newline
			'\r': true, // carriage return
		},
	}
}

// Rule is a sanitization rule that transforms or rejects input values.
type Rule interface {
	// Name returns the rule identifier.
	Name() string

	// Sanitize processes a value and returns the sanitized result.
	// Returns an error if the input should be rejected entirely.
	Sanitize(value interface{}) (interface{}, error)
}

// Pipeline is the core sanitization pipeline.
type Pipeline struct {
	config *Config
	rules  []Rule
}

// NewPipeline creates a new sanitization pipeline with the given config.
func NewPipeline(cfg *Config) *Pipeline {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Pipeline{
		config: cfg,
	}
}

// AddRule adds a custom rule to the pipeline.
func (p *Pipeline) AddRule(r Rule) {
	p.rules = append(p.rules, r)
}

// Sanitize runs the full sanitization pipeline on input data.
// Returns sanitized data or an error if input is rejected.
func (p *Pipeline) Sanitize(input map[string]interface{}) (map[string]interface{}, error) {
	if input == nil {
		return nil, nil
	}

	// 1. Total size check
	jsonBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("sanitize: failed to measure input size: %w", err)
	}
	if len(jsonBytes) > p.config.MaxInputLength {
		return nil, fmt.Errorf("sanitize: input size %d exceeds maximum %d bytes", len(jsonBytes), p.config.MaxInputLength)
	}

	// 2. Recursive sanitization (UTF-8, null bytes, control chars, depth, field length)
	sanitized, err := p.sanitizeValue(input, 0)
	if err != nil {
		return nil, err
	}

	result, ok := sanitized.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("sanitize: unexpected result type after sanitization")
	}

	// 3. Run custom rules
	for _, rule := range p.rules {
		var ruleResult interface{}
		ruleResult, err = rule.Sanitize(result)
		if err != nil {
			return nil, fmt.Errorf("sanitize rule %q: %w", rule.Name(), err)
		}
		if m, ok := ruleResult.(map[string]interface{}); ok {
			result = m
		}
	}

	return result, nil
}

// sanitizeValue recursively sanitizes a value.
func (p *Pipeline) sanitizeValue(value interface{}, depth int) (interface{}, error) {
	if depth > p.config.MaxFieldDepth {
		return nil, fmt.Errorf("sanitize: input nesting depth exceeds maximum %d", p.config.MaxFieldDepth)
	}

	switch v := value.(type) {
	case string:
		return p.sanitizeString(v)

	case map[string]interface{}:
		result := make(map[string]interface{}, len(v))
		for key, val := range v {
			// Sanitize the key too
			cleanKey, err := p.sanitizeString(key)
			if err != nil {
				return nil, fmt.Errorf("sanitize: field key %q: %w", key, err)
			}
			cleanVal, err := p.sanitizeValue(val, depth+1)
			if err != nil {
				return nil, fmt.Errorf("sanitize: field %q: %w", key, err)
			}
			result[cleanKey] = cleanVal
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			clean, err := p.sanitizeValue(item, depth+1)
			if err != nil {
				return nil, fmt.Errorf("sanitize: index %d: %w", i, err)
			}
			result[i] = clean
		}
		return result, nil

	default:
		// Numbers, booleans, nil — pass through
		return value, nil
	}
}

// sanitizeString sanitizes a single string value.
func (p *Pipeline) sanitizeString(s string) (string, error) {
	// Enforce max field length
	if len(s) > p.config.MaxFieldLength {
		return "", fmt.Errorf("string length %d exceeds maximum %d bytes", len(s), p.config.MaxFieldLength)
	}

	// Validate and fix UTF-8
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}

	// Strip null bytes and disallowed control characters
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte — skip
			i++
			continue
		}

		if r == 0 {
			// Null byte — always strip
			i += size
			continue
		}

		// Control characters (< 0x20) — check allowlist
		if r < 0x20 {
			if !p.config.AllowedControlChars[byte(r)] {
				i += size
				continue
			}
		}

		// Strip Unicode direction override characters (potential bidi attack)
		if isBidiOverride(r) {
			i += size
			continue
		}

		b.WriteRune(r)
		i += size
	}

	return b.String(), nil
}

// isBidiOverride returns true for Unicode bidirectional override characters
// that can be used in homograph/source code attacks.
func isBidiOverride(r rune) bool {
	switch r {
	case '\u202A', // Left-to-Right Embedding
		'\u202B', // Right-to-Left Embedding
		'\u202C', // Pop Directional Formatting
		'\u202D', // Left-to-Right Override
		'\u202E', // Right-to-Left Override
		'\u2066', // Left-to-Right Isolate
		'\u2067', // Right-to-Left Isolate
		'\u2068', // First Strong Isolate
		'\u2069': // Pop Directional Isolate
		return true
	}
	return false
}

// ParseAllowedControlChars converts human-readable names to a byte set.
func ParseAllowedControlChars(names []string) map[byte]bool {
	result := make(map[byte]bool)
	for _, name := range names {
		switch strings.ToLower(name) {
		case "tab":
			result['\t'] = true
		case "newline", "lf":
			result['\n'] = true
		case "cr", "carriage_return":
			result['\r'] = true
		case "backspace":
			result['\b'] = true
		case "form_feed":
			result['\f'] = true
		}
	}
	return result
}


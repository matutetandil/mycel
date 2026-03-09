// Package security provides security configuration types for Mycel.
// Security is always active — the core pipeline cannot be disabled.
// Users can only adjust thresholds and add custom WASM sanitizers.
package security

// Config holds the security configuration parsed from security/*.hcl files.
type Config struct {
	// MaxInputLength is the maximum total input size in bytes. Default: 1MB.
	MaxInputLength int

	// MaxFieldLength is the maximum length of a single string field. Default: 64KB.
	MaxFieldLength int

	// MaxFieldDepth is the maximum nesting depth for input data. Default: 20.
	MaxFieldDepth int

	// AllowedControlChars is the list of allowed control characters (e.g., "tab", "newline", "cr").
	// Default: ["tab", "newline", "cr"]
	AllowedControlChars []string

	// Sanitizers are custom WASM-based sanitizer configurations.
	Sanitizers []*SanitizerConfig

	// FlowOverrides allows per-flow threshold adjustments.
	FlowOverrides map[string]*FlowSecurityConfig
}

// SanitizerConfig defines a custom WASM-based sanitizer.
type SanitizerConfig struct {
	// Name is the sanitizer identifier.
	Name string

	// Source is the sanitizer type: "wasm".
	Source string

	// WASM is the path to the .wasm file.
	WASM string

	// Entrypoint is the WASM function name (default: "sanitize").
	Entrypoint string

	// ApplyTo is a list of flow patterns (glob) this sanitizer applies to.
	// Empty means apply to all flows.
	ApplyTo []string

	// Fields is a list of field names to sanitize.
	// Empty means apply to all string fields.
	Fields []string
}

// FlowSecurityConfig allows per-flow threshold overrides.
type FlowSecurityConfig struct {
	// MaxInputLength overrides the global max_input_length for this flow.
	MaxInputLength int

	// MaxFieldLength overrides the global max_field_length for this flow.
	MaxFieldLength int

	// Sanitizers is a list of additional sanitizer names to apply to this flow.
	Sanitizers []string
}

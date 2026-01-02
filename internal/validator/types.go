// Package validator provides custom validation logic for type fields.
// Supports regex patterns, CEL expressions, and WASM modules.
package validator

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/google/cel-go/cel"
)

// ValidatorType represents the type of validator.
type ValidatorType string

const (
	ValidatorTypeRegex ValidatorType = "regex"
	ValidatorTypeCEL   ValidatorType = "cel"
	ValidatorTypeWASM  ValidatorType = "wasm"
)

// Config represents a validator configuration from HCL.
type Config struct {
	Name       string
	Type       ValidatorType
	Pattern    string // For regex validators
	Expr       string // For CEL validators
	WASM       string // For WASM validators (file path)
	Entrypoint string // For WASM validators (function name)
	Message    string // Error message when validation fails
}

// Validator is the interface that all validators must implement.
type Validator interface {
	// Name returns the validator name.
	Name() string

	// Type returns the validator type.
	Type() ValidatorType

	// Validate checks if the value is valid.
	// Returns nil if valid, or an error with a descriptive message.
	Validate(value interface{}) error
}

// ValidationError represents a validation failure.
type ValidationError struct {
	ValidatorName string
	Value         interface{}
	Message       string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// RegexValidator validates values against a regular expression pattern.
type RegexValidator struct {
	name    string
	pattern *regexp.Regexp
	message string
}

// NewRegexValidator creates a new regex validator.
func NewRegexValidator(name, pattern, message string) (*RegexValidator, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	if message == "" {
		message = fmt.Sprintf("value does not match pattern: %s", pattern)
	}

	return &RegexValidator{
		name:    name,
		pattern: re,
		message: message,
	}, nil
}

func (v *RegexValidator) Name() string {
	return v.name
}

func (v *RegexValidator) Type() ValidatorType {
	return ValidatorTypeRegex
}

func (v *RegexValidator) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       "regex validator requires a string value",
		}
	}

	if !v.pattern.MatchString(str) {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       v.message,
		}
	}

	return nil
}

// CELValidator validates values using a CEL expression.
type CELValidator struct {
	name    string
	expr    string
	program cel.Program
	message string
}

// NewCELValidator creates a new CEL validator.
func NewCELValidator(name, expr, message string) (*CELValidator, error) {
	// Create CEL environment with 'value' variable
	env, err := cel.NewEnv(
		cel.Variable("value", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Parse and check the expression
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL compilation error: %w", issues.Err())
	}

	// Create the program
	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("CEL program creation error: %w", err)
	}

	if message == "" {
		message = fmt.Sprintf("validation failed: %s", expr)
	}

	return &CELValidator{
		name:    name,
		expr:    expr,
		program: prg,
		message: message,
	}, nil
}

func (v *CELValidator) Name() string {
	return v.name
}

func (v *CELValidator) Type() ValidatorType {
	return ValidatorTypeCEL
}

func (v *CELValidator) Validate(value interface{}) error {
	// Evaluate the CEL expression with the value
	out, _, err := v.program.Eval(map[string]interface{}{
		"value": value,
	})
	if err != nil {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       fmt.Sprintf("CEL evaluation error: %v", err),
		}
	}

	// Check if result is a boolean
	result, ok := out.Value().(bool)
	if !ok {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       "CEL expression must return a boolean",
		}
	}

	if !result {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       v.message,
		}
	}

	return nil
}

// Registry holds all registered validators.
type Registry struct {
	mu         sync.RWMutex
	validators map[string]Validator
}

// NewRegistry creates a new validator registry.
func NewRegistry() *Registry {
	return &Registry{
		validators: make(map[string]Validator),
	}
}

// Register adds a validator to the registry.
func (r *Registry) Register(v Validator) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.validators[v.Name()]; exists {
		return fmt.Errorf("validator already registered: %s", v.Name())
	}

	r.validators[v.Name()] = v
	return nil
}

// Get retrieves a validator by name.
func (r *Registry) Get(name string) (Validator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	v, ok := r.validators[name]
	return v, ok
}

// List returns all registered validator names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.validators))
	for name := range r.validators {
		names = append(names, name)
	}
	return names
}

// CreateValidator creates a validator from configuration.
func CreateValidator(cfg Config) (Validator, error) {
	switch cfg.Type {
	case ValidatorTypeRegex:
		return NewRegexValidator(cfg.Name, cfg.Pattern, cfg.Message)

	case ValidatorTypeCEL:
		return NewCELValidator(cfg.Name, cfg.Expr, cfg.Message)

	case ValidatorTypeWASM:
		return NewWASMValidator(cfg.Name, cfg.WASM, cfg.Entrypoint, cfg.Message)

	default:
		return nil, fmt.Errorf("unknown validator type: %s", cfg.Type)
	}
}

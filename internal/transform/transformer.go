// Package transform defines the transformation system for Mycel.
// It transforms data using expressions and built-in functions.
package transform

import (
	"context"
	"fmt"
	"sync"
)

// Transformer applies transformations to data.
type Transformer interface {
	// Transform applies transformation rules to input data.
	Transform(ctx context.Context, input map[string]interface{}, rules []Rule) (map[string]interface{}, error)
}

// Rule represents a single transformation rule.
type Rule struct {
	// Target is the output field path (e.g., "output.email").
	Target string

	// Expression is the transformation expression (e.g., "lower(input.email)").
	Expression string
}

// Config holds transform configuration from HCL.
type Config struct {
	// Name is the transform identifier (for named transforms).
	Name string

	// Mappings are the transformation rules.
	// Keys are output field paths, values are expressions.
	Mappings map[string]string
}

// Function represents a transform function.
// Open/Closed Principle - new functions can be added without modifying existing code.
type Function interface {
	// Name returns the function identifier.
	Name() string

	// Execute runs the function with the given arguments.
	Execute(args ...interface{}) (interface{}, error)

	// Arity returns the number of arguments expected (-1 for variadic).
	Arity() int
}

// FunctionRegistry holds transform functions.
type FunctionRegistry struct {
	mu        sync.RWMutex
	functions map[string]Function
}

// NewFunctionRegistry creates a new function registry.
func NewFunctionRegistry() *FunctionRegistry {
	return &FunctionRegistry{
		functions: make(map[string]Function),
	}
}

// Register adds a function to the registry.
func (r *FunctionRegistry) Register(fn Function) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.functions[fn.Name()] = fn
}

// Get retrieves a function by name.
func (r *FunctionRegistry) Get(name string) (Function, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.functions[name]
	return fn, ok
}

// List returns all registered function names.
func (r *FunctionRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.functions))
	for name := range r.functions {
		names = append(names, name)
	}
	return names
}

// BaseTransformer implements basic transformation logic.
type BaseTransformer struct {
	registry *FunctionRegistry
}

// NewBaseTransformer creates a new base transformer.
func NewBaseTransformer(registry *FunctionRegistry) *BaseTransformer {
	return &BaseTransformer{registry: registry}
}

// Transform applies transformation rules to input data.
func (t *BaseTransformer) Transform(ctx context.Context, input map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
	output := make(map[string]interface{})

	// Copy input to output initially
	for k, v := range input {
		output[k] = v
	}

	// Apply each rule
	for _, rule := range rules {
		value, err := t.evaluateExpression(input, output, rule.Expression)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression for %s: %w", rule.Target, err)
		}

		// Set the target field
		if err := setNestedValue(output, rule.Target, value); err != nil {
			return nil, fmt.Errorf("failed to set %s: %w", rule.Target, err)
		}
	}

	return output, nil
}

// evaluateExpression evaluates a transformation expression.
// This is a simplified implementation - a full implementation would use a proper expression parser.
func (t *BaseTransformer) evaluateExpression(input, output map[string]interface{}, expr string) (interface{}, error) {
	// For now, this is a placeholder that returns the expression as-is
	// A full implementation would:
	// 1. Parse the expression
	// 2. Resolve input.* and output.* references
	// 3. Execute function calls
	// 4. Return the result

	// Check if it's a simple field reference
	if len(expr) > 6 && expr[:6] == "input." {
		fieldPath := expr[6:]
		return getNestedValue(input, fieldPath), nil
	}

	// Return literal value
	return expr, nil
}

// setNestedValue sets a value at a nested path in a map.
func setNestedValue(data map[string]interface{}, path string, value interface{}) error {
	// Simple implementation for now - just set directly
	// A full implementation would handle nested paths like "user.address.city"
	data[path] = value
	return nil
}

// getNestedValue gets a value at a nested path in a map.
func getNestedValue(data map[string]interface{}, path string) interface{} {
	// Simple implementation for now
	if v, ok := data[path]; ok {
		return v
	}
	return nil
}

// FunctionBase provides common functionality for transform functions.
type FunctionBase struct {
	name  string
	arity int
}

// Name returns the function name.
func (f *FunctionBase) Name() string {
	return f.name
}

// Arity returns the expected number of arguments.
func (f *FunctionBase) Arity() int {
	return f.arity
}

// ValidateArgs validates the number of arguments.
func (f *FunctionBase) ValidateArgs(args []interface{}) error {
	if f.arity >= 0 && len(args) != f.arity {
		return fmt.Errorf("%s: expected %d arguments, got %d", f.name, f.arity, len(args))
	}
	return nil
}

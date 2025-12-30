// Package transform defines the transformation system for Mycel.
// It transforms data using expressions and built-in functions.
package transform

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Transformer applies transformations to data.
type Transformer interface {
	// Transform applies transformation rules to input data.
	Transform(ctx context.Context, input map[string]interface{}, rules []Rule) (map[string]interface{}, error)
}

// Rule represents a single transformation rule.
type Rule struct {
	// Target is the output field path (e.g., "email" or "user.email").
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

	// Enrichments are data lookups from external sources.
	// These are executed before mappings and results are available as enriched.*
	Enrichments []*EnrichConfig
}

// EnrichConfig holds configuration for enriching data from external sources.
// This is a copy of flow.EnrichConfig to avoid circular imports.
type EnrichConfig struct {
	// Name is the identifier for this enrichment (used as enriched.<name>).
	Name string

	// Connector is the connector to use for the lookup.
	Connector string

	// Operation is the operation to perform on the connector.
	Operation string

	// Params are the parameters to pass to the operation.
	// Keys are parameter names, values are CEL expressions.
	Params map[string]string
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

// DefaultFunctionRegistry returns a registry with all built-in functions.
func DefaultFunctionRegistry() *FunctionRegistry {
	registry := NewFunctionRegistry()
	RegisterBuiltinFunctions(registry)
	return registry
}

// BaseTransformer implements basic transformation logic.
type BaseTransformer struct {
	registry *FunctionRegistry
}

// NewBaseTransformer creates a new base transformer.
func NewBaseTransformer(registry *FunctionRegistry) *BaseTransformer {
	return &BaseTransformer{registry: registry}
}

// NewDefaultTransformer creates a transformer with default built-in functions.
func NewDefaultTransformer() *BaseTransformer {
	return NewBaseTransformer(DefaultFunctionRegistry())
}

// Transform applies transformation rules to input data.
func (t *BaseTransformer) Transform(ctx context.Context, input map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
	output := make(map[string]interface{})

	// Build evaluation context
	evalCtx := &EvalContext{
		Input:    input,
		Output:   output,
		Registry: t.registry,
	}

	// Apply each rule
	for _, rule := range rules {
		value, err := t.Evaluate(evalCtx, rule.Expression)
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

// EvalContext holds the context for expression evaluation.
type EvalContext struct {
	Input    map[string]interface{}
	Output   map[string]interface{}
	Registry *FunctionRegistry
}

// Evaluate evaluates a transformation expression.
func (t *BaseTransformer) Evaluate(ctx *EvalContext, expr string) (interface{}, error) {
	parser := &ExpressionParser{
		input: expr,
		pos:   0,
		ctx:   ctx,
	}
	return parser.Parse()
}

// ExpressionParser parses and evaluates transformation expressions.
type ExpressionParser struct {
	input string
	pos   int
	ctx   *EvalContext
}

// Parse parses and evaluates the expression.
func (p *ExpressionParser) Parse() (interface{}, error) {
	p.skipWhitespace()
	return p.parseExpression()
}

func (p *ExpressionParser) parseExpression() (interface{}, error) {
	p.skipWhitespace()

	if p.pos >= len(p.input) {
		return nil, nil
	}

	ch := p.input[p.pos]

	// String literal
	if ch == '"' || ch == '\'' {
		return p.parseStringLiteral()
	}

	// Number literal
	if unicode.IsDigit(rune(ch)) || ch == '-' {
		return p.parseNumberLiteral()
	}

	// Boolean literal or null
	if p.matchKeyword("true") {
		return true, nil
	}
	if p.matchKeyword("false") {
		return false, nil
	}
	if p.matchKeyword("null") {
		return nil, nil
	}

	// Identifier (function call or field reference)
	if unicode.IsLetter(rune(ch)) || ch == '_' {
		return p.parseIdentifier()
	}

	return nil, fmt.Errorf("unexpected character at position %d: %c", p.pos, ch)
}

func (p *ExpressionParser) parseStringLiteral() (interface{}, error) {
	quote := p.input[p.pos]
	p.pos++ // skip opening quote

	var result strings.Builder
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == byte(quote) {
			p.pos++ // skip closing quote
			return result.String(), nil
		}
		if ch == '\\' && p.pos+1 < len(p.input) {
			p.pos++
			switch p.input[p.pos] {
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case 'r':
				result.WriteByte('\r')
			case '\\':
				result.WriteByte('\\')
			case '"':
				result.WriteByte('"')
			case '\'':
				result.WriteByte('\'')
			default:
				result.WriteByte(p.input[p.pos])
			}
			p.pos++
			continue
		}
		result.WriteByte(ch)
		p.pos++
	}
	return nil, fmt.Errorf("unterminated string literal")
}

func (p *ExpressionParser) parseNumberLiteral() (interface{}, error) {
	start := p.pos
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
	}

	hasDecimal := false
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if unicode.IsDigit(rune(ch)) {
			p.pos++
		} else if ch == '.' && !hasDecimal {
			hasDecimal = true
			p.pos++
		} else {
			break
		}
	}

	numStr := p.input[start:p.pos]
	if hasDecimal {
		val, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", numStr)
		}
		return val, nil
	}

	val, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid integer: %s", numStr)
	}
	return int(val), nil
}

func (p *ExpressionParser) matchKeyword(keyword string) bool {
	if p.pos+len(keyword) > len(p.input) {
		return false
	}
	if p.input[p.pos:p.pos+len(keyword)] != keyword {
		return false
	}
	// Check that keyword is not part of a longer identifier
	if p.pos+len(keyword) < len(p.input) {
		next := rune(p.input[p.pos+len(keyword)])
		if unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' {
			return false
		}
	}
	p.pos += len(keyword)
	return true
}

func (p *ExpressionParser) parseIdentifier() (interface{}, error) {
	start := p.pos
	for p.pos < len(p.input) {
		ch := rune(p.input[p.pos])
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '.' {
			p.pos++
		} else {
			break
		}
	}

	identifier := p.input[start:p.pos]
	p.skipWhitespace()

	// Check if it's a function call
	if p.pos < len(p.input) && p.input[p.pos] == '(' {
		return p.parseFunctionCall(identifier)
	}

	// It's a field reference
	return p.resolveReference(identifier)
}

func (p *ExpressionParser) parseFunctionCall(name string) (interface{}, error) {
	p.pos++ // skip '('
	p.skipWhitespace()

	var args []interface{}

	// Parse arguments
	for p.pos < len(p.input) && p.input[p.pos] != ')' {
		arg, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("error parsing argument for %s: %w", name, err)
		}
		args = append(args, arg)

		p.skipWhitespace()
		if p.pos < len(p.input) && p.input[p.pos] == ',' {
			p.pos++ // skip ','
			p.skipWhitespace()
		}
	}

	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("unclosed function call: %s", name)
	}
	p.pos++ // skip ')'

	// Get function from registry
	fn, ok := p.ctx.Registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown function: %s", name)
	}

	// Execute function
	return fn.Execute(args...)
}

func (p *ExpressionParser) resolveReference(ref string) (interface{}, error) {
	parts := strings.Split(ref, ".")

	if len(parts) == 0 {
		return nil, fmt.Errorf("empty reference")
	}

	var data map[string]interface{}
	startIdx := 0

	// Determine the data source
	switch parts[0] {
	case "input":
		data = p.ctx.Input
		startIdx = 1
	case "output":
		data = p.ctx.Output
		startIdx = 1
	default:
		// Default to input if no prefix
		data = p.ctx.Input
	}

	return getNestedValue(data, parts[startIdx:]), nil
}

func (p *ExpressionParser) skipWhitespace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

// setNestedValue sets a value at a nested path in a map.
func setNestedValue(data map[string]interface{}, path string, value interface{}) error {
	parts := strings.Split(path, ".")

	// Navigate to the parent, creating nested maps as needed
	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			// Create nested map
			next := make(map[string]interface{})
			current[part] = next
			current = next
		}
	}

	// Set the final value
	current[parts[len(parts)-1]] = value
	return nil
}

// getNestedValue gets a value at a nested path in a map.
func getNestedValue(data map[string]interface{}, parts []string) interface{} {
	if len(parts) == 0 {
		return data
	}

	current := data
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return nil
		}

		// If this is the last part, return the value
		if i == len(parts)-1 {
			return val
		}

		// Otherwise, navigate deeper
		next, ok := val.(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}

	return current
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

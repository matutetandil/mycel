// Package transform provides CEL-based transformation for Mycel.
package transform

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"github.com/google/uuid"
)

// CELTransformer implements transformation using Google's Common Expression Language.
type CELTransformer struct {
	env *cel.Env

	// Cache compiled programs for reuse
	mu       sync.RWMutex
	programs map[string]cel.Program
}

// NewCELTransformer creates a new CEL-based transformer with custom Mycel functions.
func NewCELTransformer() (*CELTransformer, error) {
	// Create CEL environment with full standard library + extensions + custom functions
	env, err := cel.NewEnv(
		// CEL Standard Extensions - provides full CEL functionality
		ext.Strings(),   // charAt, indexOf, lastIndexOf, join, quote, replace, split, substring, trim, upperAscii, lowerAscii, reverse
		ext.Encoders(),  // base64.encode, base64.decode
		ext.Math(),      // math.least, math.greatest, math.ceil, math.floor, math.round, math.abs, math.sign, math.isNaN, math.isInf
		ext.Lists(),     // lists.range, slice, flatten
		ext.Sets(),      // sets.contains, sets.equivalent, sets.intersects

		// Input variable - the request data
		cel.Variable("input", cel.MapType(cel.StringType, cel.DynType)),

		// Output variable - for referencing already-set output fields
		cel.Variable("output", cel.MapType(cel.StringType, cel.DynType)),

		// Context variable - for request context (headers, path params, etc.)
		cel.Variable("ctx", cel.MapType(cel.StringType, cel.DynType)),

		// Enriched variable - for data fetched from external sources
		cel.Variable("enriched", cel.MapType(cel.StringType, cel.DynType)),

		// Custom Mycel functions
		cel.Function("uuid",
			cel.Overload("uuid_generate",
				[]*cel.Type{},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.String(uuid.New().String())
				}),
			),
		),

		cel.Function("now",
			cel.Overload("now_timestamp",
				[]*cel.Type{},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.String(time.Now().UTC().Format(time.RFC3339))
				}),
			),
		),

		cel.Function("now_unix",
			cel.Overload("now_unix_timestamp",
				[]*cel.Type{},
				cel.IntType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Int(time.Now().Unix())
				}),
			),
		),

		cel.Function("trim",
			cel.Overload("trim_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s := string(val.(types.String))
					return types.String(strings.TrimSpace(s))
				}),
			),
		),

		cel.Function("lower",
			cel.Overload("lower_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s := string(val.(types.String))
					return types.String(strings.ToLower(s))
				}),
			),
		),

		cel.Function("upper",
			cel.Overload("upper_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s := string(val.(types.String))
					return types.String(strings.ToUpper(s))
				}),
			),
		),

		cel.Function("replace",
			cel.Overload("replace_string",
				[]*cel.Type{cel.StringType, cel.StringType, cel.StringType},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					s := string(args[0].(types.String))
					old := string(args[1].(types.String))
					new := string(args[2].(types.String))
					return types.String(strings.ReplaceAll(s, old, new))
				}),
			),
		),

		cel.Function("split",
			cel.Overload("split_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.ListType(cel.StringType),
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					s := string(lhs.(types.String))
					sep := string(rhs.(types.String))
					parts := strings.Split(s, sep)
					result := make([]ref.Val, len(parts))
					for i, p := range parts {
						result[i] = types.String(p)
					}
					return types.NewStringList(types.DefaultTypeAdapter, parts)
				}),
			),
		),

		cel.Function("join",
			cel.Overload("join_list",
				[]*cel.Type{cel.ListType(cel.StringType), cel.StringType},
				cel.StringType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					list := lhs.(interface{ Size() ref.Val })
					sep := string(rhs.(types.String))
					var parts []string
					size, _ := list.Size().ConvertToNative(reflect.TypeOf(int64(0)))
					listVal := lhs.(interface {
						Get(ref.Val) ref.Val
					})
					for i := int64(0); i < size.(int64); i++ {
						item := listVal.Get(types.Int(i))
						parts = append(parts, string(item.(types.String)))
					}
					return types.String(strings.Join(parts, sep))
				}),
			),
		),

		cel.Function("default",
			cel.Overload("default_value",
				[]*cel.Type{cel.DynType, cel.DynType},
				cel.DynType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					if lhs == nil || lhs == types.NullValue {
						return rhs
					}
					// Check for empty string
					if s, ok := lhs.(types.String); ok && string(s) == "" {
						return rhs
					}
					return lhs
				}),
			),
		),

		cel.Function("coalesce",
			cel.Overload("coalesce_values",
				[]*cel.Type{cel.DynType, cel.DynType},
				cel.DynType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					if lhs == nil || lhs == types.NullValue {
						return rhs
					}
					if s, ok := lhs.(types.String); ok && string(s) == "" {
						return rhs
					}
					return lhs
				}),
			),
		),

		cel.Function("hash_sha256",
			cel.Overload("hash_sha256_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s := string(val.(types.String))
					// Simple hash - in production use crypto/sha256
					hash := fmt.Sprintf("%x", hashString(s))
					return types.String(hash)
				}),
			),
		),

		cel.Function("substring",
			cel.Overload("substring_string",
				[]*cel.Type{cel.StringType, cel.IntType, cel.IntType},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					s := string(args[0].(types.String))
					start := int(args[1].(types.Int))
					end := int(args[2].(types.Int))
					if start < 0 {
						start = 0
					}
					if end > len(s) {
						end = len(s)
					}
					if start >= end {
						return types.String("")
					}
					return types.String(s[start:end])
				}),
			),
		),

		cel.Function("len",
			cel.Overload("len_string",
				[]*cel.Type{cel.StringType},
				cel.IntType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s := string(val.(types.String))
					return types.Int(len(s))
				}),
			),
		),

		cel.Function("format_date",
			cel.Overload("format_date_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					dateStr := string(lhs.(types.String))
					format := string(rhs.(types.String))
					// Parse ISO date and reformat
					t, err := time.Parse(time.RFC3339, dateStr)
					if err != nil {
						return types.String(dateStr)
					}
					return types.String(t.Format(goTimeFormat(format)))
				}),
			),
		),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELTransformer{
		env:      env,
		programs: make(map[string]cel.Program),
	}, nil
}

// Compile compiles a CEL expression and caches the program.
// Call this at startup/config load time for early error detection.
func (t *CELTransformer) Compile(expr string) (cel.Program, error) {
	t.mu.RLock()
	if prog, ok := t.programs[expr]; ok {
		t.mu.RUnlock()
		return prog, nil
	}
	t.mu.RUnlock()

	// Parse and check the expression
	ast, issues := t.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL compile error: %w", issues.Err())
	}

	// Create the program
	prog, err := t.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("CEL program error: %w", err)
	}

	// Cache it
	t.mu.Lock()
	t.programs[expr] = prog
	t.mu.Unlock()

	return prog, nil
}

// Evaluate evaluates a CEL expression with the given input.
func (t *CELTransformer) Evaluate(ctx context.Context, expr string, input map[string]interface{}) (interface{}, error) {
	prog, err := t.Compile(expr)
	if err != nil {
		return nil, err
	}

	// Build activation (variables available to the expression)
	activation := map[string]interface{}{
		"input":  input,
		"output": make(map[string]interface{}),
		"ctx":    make(map[string]interface{}),
	}

	// Evaluate
	result, _, err := prog.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("CEL eval error: %w", err)
	}

	return result.Value(), nil
}

// Transform applies transformation rules to input data using CEL.
func (t *CELTransformer) Transform(ctx context.Context, input map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
	output := make(map[string]interface{})

	// Build activation with input and growing output
	activation := map[string]interface{}{
		"input":  input,
		"output": output,
		"ctx":    make(map[string]interface{}),
	}

	for _, rule := range rules {
		// Compile/get cached program
		prog, err := t.Compile(rule.Expression)
		if err != nil {
			return nil, fmt.Errorf("failed to compile expression for '%s': %w", rule.Target, err)
		}

		// Evaluate with current activation (output grows with each rule)
		result, _, err := prog.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression for '%s': %w", rule.Target, err)
		}

		// Set the result in output
		value := result.Value()
		if err := setNestedValue(output, rule.Target, value); err != nil {
			return nil, fmt.Errorf("failed to set '%s': %w", rule.Target, err)
		}

		// Update activation with new output value
		activation["output"] = output
	}

	return output, nil
}

// ValidateExpression checks if a CEL expression is valid without executing it.
func (t *CELTransformer) ValidateExpression(expr string) error {
	_, err := t.Compile(expr)
	return err
}

// hashString is a simple string hash (for demo - use crypto/sha256 in production)
func hashString(s string) uint64 {
	var h uint64 = 5381
	for _, c := range s {
		h = ((h << 5) + h) + uint64(c)
	}
	return h
}

// goTimeFormat converts common format strings to Go time format.
func goTimeFormat(format string) string {
	replacements := map[string]string{
		"YYYY": "2006",
		"MM":   "01",
		"DD":   "02",
		"HH":   "15",
		"mm":   "04",
		"ss":   "05",
	}
	result := format
	for k, v := range replacements {
		result = strings.ReplaceAll(result, k, v)
	}
	return result
}

// EvaluateExpression evaluates a single CEL expression with input and enriched data.
// This is used for evaluating enrich params before making the enrichment call.
func (t *CELTransformer) EvaluateExpression(ctx context.Context, input map[string]interface{}, enriched map[string]interface{}, expr string) (interface{}, error) {
	prog, err := t.Compile(expr)
	if err != nil {
		return nil, err
	}

	// Build activation
	if enriched == nil {
		enriched = make(map[string]interface{})
	}

	activation := map[string]interface{}{
		"input":    input,
		"output":   make(map[string]interface{}),
		"ctx":      make(map[string]interface{}),
		"enriched": enriched,
	}

	// Evaluate
	result, _, err := prog.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("CEL eval error: %w", err)
	}

	return result.Value(), nil
}

// TransformWithEnriched applies transformation rules with enriched data available.
func (t *CELTransformer) TransformWithEnriched(ctx context.Context, input map[string]interface{}, enriched map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
	output := make(map[string]interface{})

	// Ensure enriched is not nil
	if enriched == nil {
		enriched = make(map[string]interface{})
	}

	// Build activation with input, output, and enriched data
	activation := map[string]interface{}{
		"input":    input,
		"output":   output,
		"ctx":      make(map[string]interface{}),
		"enriched": enriched,
	}

	for _, rule := range rules {
		// Compile/get cached program
		prog, err := t.Compile(rule.Expression)
		if err != nil {
			return nil, fmt.Errorf("failed to compile expression for '%s': %w", rule.Target, err)
		}

		// Evaluate with current activation (output grows with each rule)
		result, _, err := prog.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression for '%s': %w", rule.Target, err)
		}

		// Set the result in output
		value := result.Value()
		if err := setNestedValue(output, rule.Target, value); err != nil {
			return nil, fmt.Errorf("failed to set '%s': %w", rule.Target, err)
		}

		// Update activation with new output value
		activation["output"] = output
	}

	return output, nil
}

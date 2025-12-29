package transform

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// RegisterBuiltinFunctions registers all built-in transform functions.
func RegisterBuiltinFunctions(registry *FunctionRegistry) {
	// String functions
	registry.Register(&LowerFunc{})
	registry.Register(&UpperFunc{})
	registry.Register(&TrimFunc{})
	registry.Register(&TrimPrefixFunc{})
	registry.Register(&TrimSuffixFunc{})
	registry.Register(&ReplaceFunc{})
	registry.Register(&SubstringFunc{})
	registry.Register(&ConcatFunc{})
	registry.Register(&SplitFunc{})
	registry.Register(&JoinFunc{})

	// Date/Time functions
	registry.Register(&NowFunc{})
	registry.Register(&FormatDateFunc{})
	registry.Register(&ParseDateFunc{})

	// ID generation functions
	registry.Register(&UUIDFunc{})
	registry.Register(&RandomStringFunc{})

	// Type conversion functions
	registry.Register(&ToStringFunc{})
	registry.Register(&ToIntFunc{})
	registry.Register(&ToFloatFunc{})
	registry.Register(&ToBoolFunc{})

	// Utility functions
	registry.Register(&CoalesceFunc{})
	registry.Register(&DefaultFunc{})
	registry.Register(&IfFunc{})
	registry.Register(&LenFunc{})
}

// ---- String Functions ----

// LowerFunc converts string to lowercase.
type LowerFunc struct{ FunctionBase }

func (f *LowerFunc) Name() string { return "lower" }
func (f *LowerFunc) Arity() int   { return 1 }
func (f *LowerFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("lower: expected 1 argument, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("lower: argument must be string")
	}
	return strings.ToLower(s), nil
}

// UpperFunc converts string to uppercase.
type UpperFunc struct{ FunctionBase }

func (f *UpperFunc) Name() string { return "upper" }
func (f *UpperFunc) Arity() int   { return 1 }
func (f *UpperFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("upper: expected 1 argument, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("upper: argument must be string")
	}
	return strings.ToUpper(s), nil
}

// TrimFunc removes leading and trailing whitespace.
type TrimFunc struct{ FunctionBase }

func (f *TrimFunc) Name() string { return "trim" }
func (f *TrimFunc) Arity() int   { return 1 }
func (f *TrimFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("trim: expected 1 argument, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("trim: argument must be string")
	}
	return strings.TrimSpace(s), nil
}

// TrimPrefixFunc removes a prefix from a string.
type TrimPrefixFunc struct{ FunctionBase }

func (f *TrimPrefixFunc) Name() string { return "trim_prefix" }
func (f *TrimPrefixFunc) Arity() int   { return 2 }
func (f *TrimPrefixFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("trim_prefix: expected 2 arguments, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("trim_prefix: first argument must be string")
	}
	prefix, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("trim_prefix: second argument must be string")
	}
	return strings.TrimPrefix(s, prefix), nil
}

// TrimSuffixFunc removes a suffix from a string.
type TrimSuffixFunc struct{ FunctionBase }

func (f *TrimSuffixFunc) Name() string { return "trim_suffix" }
func (f *TrimSuffixFunc) Arity() int   { return 2 }
func (f *TrimSuffixFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("trim_suffix: expected 2 arguments, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("trim_suffix: first argument must be string")
	}
	suffix, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("trim_suffix: second argument must be string")
	}
	return strings.TrimSuffix(s, suffix), nil
}

// ReplaceFunc replaces occurrences of a substring.
type ReplaceFunc struct{ FunctionBase }

func (f *ReplaceFunc) Name() string { return "replace" }
func (f *ReplaceFunc) Arity() int   { return 3 }
func (f *ReplaceFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("replace: expected 3 arguments, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("replace: first argument must be string")
	}
	old, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("replace: second argument must be string")
	}
	new, ok := args[2].(string)
	if !ok {
		return nil, fmt.Errorf("replace: third argument must be string")
	}
	return strings.ReplaceAll(s, old, new), nil
}

// SubstringFunc extracts a substring.
type SubstringFunc struct{ FunctionBase }

func (f *SubstringFunc) Name() string { return "substring" }
func (f *SubstringFunc) Arity() int   { return -1 } // 2 or 3 args
func (f *SubstringFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("substring: expected 2 or 3 arguments, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("substring: first argument must be string")
	}
	start, ok := toInt(args[1])
	if !ok {
		return nil, fmt.Errorf("substring: second argument must be integer")
	}
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return "", nil
	}

	if len(args) == 3 {
		length, ok := toInt(args[2])
		if !ok {
			return nil, fmt.Errorf("substring: third argument must be integer")
		}
		end := start + length
		if end > len(s) {
			end = len(s)
		}
		return s[start:end], nil
	}
	return s[start:], nil
}

// ConcatFunc concatenates strings.
type ConcatFunc struct{ FunctionBase }

func (f *ConcatFunc) Name() string { return "concat" }
func (f *ConcatFunc) Arity() int   { return -1 } // variadic
func (f *ConcatFunc) Execute(args ...interface{}) (interface{}, error) {
	var result strings.Builder
	for _, arg := range args {
		result.WriteString(fmt.Sprintf("%v", arg))
	}
	return result.String(), nil
}

// SplitFunc splits a string by delimiter.
type SplitFunc struct{ FunctionBase }

func (f *SplitFunc) Name() string { return "split" }
func (f *SplitFunc) Arity() int   { return 2 }
func (f *SplitFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("split: expected 2 arguments, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("split: first argument must be string")
	}
	sep, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("split: second argument must be string")
	}
	return strings.Split(s, sep), nil
}

// JoinFunc joins array elements with delimiter.
type JoinFunc struct{ FunctionBase }

func (f *JoinFunc) Name() string { return "join" }
func (f *JoinFunc) Arity() int   { return 2 }
func (f *JoinFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("join: expected 2 arguments, got %d", len(args))
	}
	sep, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("join: second argument must be string")
	}

	// Handle different array types
	switch arr := args[0].(type) {
	case []string:
		return strings.Join(arr, sep), nil
	case []interface{}:
		strs := make([]string, len(arr))
		for i, v := range arr {
			strs[i] = fmt.Sprintf("%v", v)
		}
		return strings.Join(strs, sep), nil
	default:
		return nil, fmt.Errorf("join: first argument must be array")
	}
}

// ---- Date/Time Functions ----

// NowFunc returns current timestamp.
type NowFunc struct{ FunctionBase }

func (f *NowFunc) Name() string { return "now" }
func (f *NowFunc) Arity() int   { return 0 }
func (f *NowFunc) Execute(args ...interface{}) (interface{}, error) {
	return time.Now().UTC().Format(time.RFC3339), nil
}

// FormatDateFunc formats a date.
type FormatDateFunc struct{ FunctionBase }

func (f *FormatDateFunc) Name() string { return "format_date" }
func (f *FormatDateFunc) Arity() int   { return 2 }
func (f *FormatDateFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("format_date: expected 2 arguments, got %d", len(args))
	}
	layout, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("format_date: second argument must be string")
	}

	var t time.Time
	switch v := args[0].(type) {
	case time.Time:
		t = v
	case string:
		var err error
		t, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("format_date: invalid date string: %w", err)
		}
	default:
		return nil, fmt.Errorf("format_date: first argument must be time or string")
	}

	return t.Format(layout), nil
}

// ParseDateFunc parses a date string.
type ParseDateFunc struct{ FunctionBase }

func (f *ParseDateFunc) Name() string { return "parse_date" }
func (f *ParseDateFunc) Arity() int   { return 2 }
func (f *ParseDateFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("parse_date: expected 2 arguments, got %d", len(args))
	}
	value, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("parse_date: first argument must be string")
	}
	layout, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("parse_date: second argument must be string")
	}

	t, err := time.Parse(layout, value)
	if err != nil {
		return nil, fmt.Errorf("parse_date: %w", err)
	}
	return t.Format(time.RFC3339), nil
}

// ---- ID Generation Functions ----

// UUIDFunc generates a UUID v4.
type UUIDFunc struct{ FunctionBase }

func (f *UUIDFunc) Name() string { return "uuid" }
func (f *UUIDFunc) Arity() int   { return 0 }
func (f *UUIDFunc) Execute(args ...interface{}) (interface{}, error) {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		return nil, fmt.Errorf("uuid: failed to generate: %w", err)
	}
	// Set version (4) and variant bits
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// RandomStringFunc generates a random string.
type RandomStringFunc struct{ FunctionBase }

func (f *RandomStringFunc) Name() string { return "random_string" }
func (f *RandomStringFunc) Arity() int   { return 1 }
func (f *RandomStringFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("random_string: expected 1 argument, got %d", len(args))
	}
	length, ok := toInt(args[0])
	if !ok {
		return nil, fmt.Errorf("random_string: argument must be integer")
	}
	if length <= 0 {
		return "", nil
	}

	bytes := make([]byte, (length+1)/2)
	_, err := rand.Read(bytes)
	if err != nil {
		return nil, fmt.Errorf("random_string: failed to generate: %w", err)
	}
	return hex.EncodeToString(bytes)[:length], nil
}

// ---- Type Conversion Functions ----

// ToStringFunc converts to string.
type ToStringFunc struct{ FunctionBase }

func (f *ToStringFunc) Name() string { return "to_string" }
func (f *ToStringFunc) Arity() int   { return 1 }
func (f *ToStringFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("to_string: expected 1 argument, got %d", len(args))
	}
	return fmt.Sprintf("%v", args[0]), nil
}

// ToIntFunc converts to integer.
type ToIntFunc struct{ FunctionBase }

func (f *ToIntFunc) Name() string { return "to_int" }
func (f *ToIntFunc) Arity() int   { return 1 }
func (f *ToIntFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("to_int: expected 1 argument, got %d", len(args))
	}
	val, ok := toInt(args[0])
	if !ok {
		return nil, fmt.Errorf("to_int: cannot convert to integer")
	}
	return val, nil
}

// ToFloatFunc converts to float.
type ToFloatFunc struct{ FunctionBase }

func (f *ToFloatFunc) Name() string { return "to_float" }
func (f *ToFloatFunc) Arity() int   { return 1 }
func (f *ToFloatFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("to_float: expected 1 argument, got %d", len(args))
	}
	val, ok := toFloat(args[0])
	if !ok {
		return nil, fmt.Errorf("to_float: cannot convert to float")
	}
	return val, nil
}

// ToBoolFunc converts to boolean.
type ToBoolFunc struct{ FunctionBase }

func (f *ToBoolFunc) Name() string { return "to_bool" }
func (f *ToBoolFunc) Arity() int   { return 1 }
func (f *ToBoolFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("to_bool: expected 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case bool:
		return v, nil
	case string:
		return v == "true" || v == "1" || v == "yes", nil
	case int, int64:
		return v != 0, nil
	case float64:
		return v != 0, nil
	default:
		return args[0] != nil, nil
	}
}

// ---- Utility Functions ----

// CoalesceFunc returns the first non-nil value.
type CoalesceFunc struct{ FunctionBase }

func (f *CoalesceFunc) Name() string { return "coalesce" }
func (f *CoalesceFunc) Arity() int   { return -1 } // variadic
func (f *CoalesceFunc) Execute(args ...interface{}) (interface{}, error) {
	for _, arg := range args {
		if arg != nil {
			if s, ok := arg.(string); ok && s == "" {
				continue
			}
			return arg, nil
		}
	}
	return nil, nil
}

// DefaultFunc returns the second arg if first is nil/empty.
type DefaultFunc struct{ FunctionBase }

func (f *DefaultFunc) Name() string { return "default" }
func (f *DefaultFunc) Arity() int   { return 2 }
func (f *DefaultFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("default: expected 2 arguments, got %d", len(args))
	}
	if args[0] == nil {
		return args[1], nil
	}
	if s, ok := args[0].(string); ok && s == "" {
		return args[1], nil
	}
	return args[0], nil
}

// IfFunc returns second arg if first is true, else third arg.
type IfFunc struct{ FunctionBase }

func (f *IfFunc) Name() string { return "if" }
func (f *IfFunc) Arity() int   { return 3 }
func (f *IfFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("if: expected 3 arguments, got %d", len(args))
	}
	cond, _ := args[0].(bool)
	if cond {
		return args[1], nil
	}
	return args[2], nil
}

// LenFunc returns the length of a string or array.
type LenFunc struct{ FunctionBase }

func (f *LenFunc) Name() string { return "len" }
func (f *LenFunc) Arity() int   { return 1 }
func (f *LenFunc) Execute(args ...interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("len: expected 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case string:
		return len(v), nil
	case []interface{}:
		return len(v), nil
	case []string:
		return len(v), nil
	case map[string]interface{}:
		return len(v), nil
	default:
		return 0, nil
	}
}

// ---- Helper functions ----

func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case string:
		var i int
		_, err := fmt.Sscanf(val, "%d", &i)
		return i, err == nil
	default:
		return 0, false
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		var f float64
		_, err := fmt.Sscanf(val, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

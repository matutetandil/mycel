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
	"github.com/google/cel-go/common/types/traits"
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
	return NewCELTransformerWithOptions()
}

// NewCELTransformerWithOptions creates a new CEL-based transformer with additional options.
// Use this to add custom WASM functions to the CEL environment.
func NewCELTransformerWithOptions(additionalOptions ...cel.EnvOption) (*CELTransformer, error) {
	// Build the complete list of options
	options := baseCELOptions()
	options = append(options, additionalOptions...)

	// Create CEL environment
	env, err := cel.NewEnv(options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELTransformer{
		env:      env,
		programs: make(map[string]cel.Program),
	}, nil
}

// baseCELOptions returns the base CEL environment options with all Mycel built-in functions.
func baseCELOptions() []cel.EnvOption {
	return []cel.EnvOption{
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

		// Step variable - for intermediate connector call results
		cel.Variable("step", cel.MapType(cel.StringType, cel.DynType)),

		// Result variable - for aspect conditions (after execution)
		cel.Variable("result", cel.MapType(cel.StringType, cel.DynType)),

		// Error variable - for aspect conditions (after execution with error)
		cel.Variable("error", cel.StringType),

		// Flow metadata variables for aspects
		cel.Variable("_flow", cel.StringType),
		cel.Variable("_operation", cel.StringType),
		cel.Variable("_target", cel.StringType),
		cel.Variable("_timestamp", cel.IntType),

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

		// Array/List helper functions

		cel.Function("first",
			cel.Overload("first_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.DynType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					if list.Size().(types.Int) == 0 {
						return types.NullValue
					}
					return list.Get(types.Int(0))
				}),
			),
		),

		cel.Function("last",
			cel.Overload("last_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.DynType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					size := list.Size().(types.Int)
					if size == 0 {
						return types.NullValue
					}
					return list.Get(size - 1)
				}),
			),
		),

		cel.Function("flatten",
			cel.Overload("flatten_list",
				[]*cel.Type{cel.ListType(cel.ListType(cel.DynType))},
				cel.ListType(cel.DynType),
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					var result []ref.Val
					size := int(list.Size().(types.Int))
					for i := 0; i < size; i++ {
						item := list.Get(types.Int(i))
						if innerList, ok := item.(traits.Lister); ok {
							innerSize := int(innerList.Size().(types.Int))
							for j := 0; j < innerSize; j++ {
								result = append(result, innerList.Get(types.Int(j)))
							}
						} else {
							result = append(result, item)
						}
					}
					return types.NewDynamicList(types.DefaultTypeAdapter, result)
				}),
			),
		),

		cel.Function("unique",
			cel.Overload("unique_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.ListType(cel.DynType),
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					seen := make(map[interface{}]bool)
					var result []ref.Val
					size := int(list.Size().(types.Int))
					for i := 0; i < size; i++ {
						item := list.Get(types.Int(i))
						key := item.Value()
						if !seen[key] {
							seen[key] = true
							result = append(result, item)
						}
					}
					return types.NewDynamicList(types.DefaultTypeAdapter, result)
				}),
			),
		),

		cel.Function("reverse",
			cel.Overload("reverse_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.ListType(cel.DynType),
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					size := int(list.Size().(types.Int))
					result := make([]ref.Val, size)
					for i := 0; i < size; i++ {
						result[size-1-i] = list.Get(types.Int(i))
					}
					return types.NewDynamicList(types.DefaultTypeAdapter, result)
				}),
			),
		),

		cel.Function("pluck",
			cel.Overload("pluck_list_string",
				[]*cel.Type{cel.ListType(cel.MapType(cel.StringType, cel.DynType)), cel.StringType},
				cel.ListType(cel.DynType),
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					list := lhs.(traits.Lister)
					key := string(rhs.(types.String))
					var result []ref.Val
					size := int(list.Size().(types.Int))
					for i := 0; i < size; i++ {
						item := list.Get(types.Int(i))
						if mapper, ok := item.(traits.Mapper); ok {
							val := mapper.Get(types.String(key))
							result = append(result, val)
						}
					}
					return types.NewDynamicList(types.DefaultTypeAdapter, result)
				}),
			),
		),

		cel.Function("sum",
			cel.Overload("sum_list_int",
				[]*cel.Type{cel.ListType(cel.IntType)},
				cel.IntType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					var sum int64
					size := int(list.Size().(types.Int))
					for i := 0; i < size; i++ {
						item := list.Get(types.Int(i))
						if num, ok := item.(types.Int); ok {
							sum += int64(num)
						}
					}
					return types.Int(sum)
				}),
			),
			cel.Overload("sum_list_double",
				[]*cel.Type{cel.ListType(cel.DoubleType)},
				cel.DoubleType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					var sum float64
					size := int(list.Size().(types.Int))
					for i := 0; i < size; i++ {
						item := list.Get(types.Int(i))
						if num, ok := item.(types.Double); ok {
							sum += float64(num)
						}
					}
					return types.Double(sum)
				}),
			),
		),

		cel.Function("avg",
			cel.Overload("avg_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.DoubleType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					size := int(list.Size().(types.Int))
					if size == 0 {
						return types.Double(0)
					}
					var sum float64
					for i := 0; i < size; i++ {
						item := list.Get(types.Int(i))
						switch v := item.(type) {
						case types.Int:
							sum += float64(v)
						case types.Double:
							sum += float64(v)
						}
					}
					return types.Double(sum / float64(size))
				}),
			),
		),

		cel.Function("min_val",
			cel.Overload("min_val_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.DynType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					size := int(list.Size().(types.Int))
					if size == 0 {
						return types.NullValue
					}
					minVal := list.Get(types.Int(0))
					for i := 1; i < size; i++ {
						item := list.Get(types.Int(i))
						if compare(item, minVal) < 0 {
							minVal = item
						}
					}
					return minVal
				}),
			),
		),

		cel.Function("max_val",
			cel.Overload("max_val_list",
				[]*cel.Type{cel.ListType(cel.DynType)},
				cel.DynType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					list := val.(traits.Lister)
					size := int(list.Size().(types.Int))
					if size == 0 {
						return types.NullValue
					}
					maxVal := list.Get(types.Int(0))
					for i := 1; i < size; i++ {
						item := list.Get(types.Int(i))
						if compare(item, maxVal) > 0 {
							maxVal = item
						}
					}
					return maxVal
				}),
			),
		),

		// sort_by(list, key) - Sort list of maps by a key (ascending)
		cel.Function("sort_by",
			cel.Overload("sort_by_list_string",
				[]*cel.Type{cel.ListType(cel.DynType), cel.StringType},
				cel.ListType(cel.DynType),
				cel.BinaryBinding(func(listVal, keyVal ref.Val) ref.Val {
					list := listVal.(traits.Lister)
					key := string(keyVal.(types.String))
					size := int(list.Size().(types.Int))
					if size == 0 {
						return listVal
					}

					// Copy list items to slice for sorting
					items := make([]ref.Val, size)
					for i := 0; i < size; i++ {
						items[i] = list.Get(types.Int(i))
					}

					// Sort using bubble sort (simple, stable)
					for i := 0; i < size-1; i++ {
						for j := 0; j < size-i-1; j++ {
							item1 := items[j].(traits.Mapper)
							item2 := items[j+1].(traits.Mapper)
							val1 := item1.Get(types.String(key))
							val2 := item2.Get(types.String(key))
							if compare(val1, val2) > 0 {
								items[j], items[j+1] = items[j+1], items[j]
							}
						}
					}

					return types.DefaultTypeAdapter.NativeToValue(items)
				}),
			),
		),

		// merge(map1, map2, ...) - Merge multiple maps into one (later values override earlier)
		cel.Function("merge",
			// Two maps
			cel.Overload("merge_two_maps",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType)},
				cel.MapType(cel.StringType, cel.DynType),
				cel.BinaryBinding(func(m1, m2 ref.Val) ref.Val {
					return mergeMaps(m1, m2)
				}),
			),
			// Three maps
			cel.Overload("merge_three_maps",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType)},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					result := mergeMaps(args[0], args[1])
					return mergeMaps(result, args[2])
				}),
			),
			// Four maps
			cel.Overload("merge_four_maps",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType), cel.MapType(cel.StringType, cel.DynType)},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					result := mergeMaps(args[0], args[1])
					result = mergeMaps(result, args[2])
					return mergeMaps(result, args[3])
				}),
			),
		),

		// omit(map, key1, key2, ...) - Return map without specified keys
		cel.Function("omit",
			cel.Overload("omit_map_string",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.BinaryBinding(func(mapVal, keyVal ref.Val) ref.Val {
					return omitKeys(mapVal, keyVal)
				}),
			),
			cel.Overload("omit_map_two_strings",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					result := omitKeys(args[0], args[1])
					return omitKeys(result, args[2])
				}),
			),
			cel.Overload("omit_map_three_strings",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.StringType, cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					result := omitKeys(args[0], args[1])
					result = omitKeys(result, args[2])
					return omitKeys(result, args[3])
				}),
			),
		),

		// pick(map, key1, key2, ...) - Return map with only specified keys
		cel.Function("pick",
			cel.Overload("pick_map_string",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.BinaryBinding(func(mapVal, keyVal ref.Val) ref.Val {
					return pickKeys(mapVal, keyVal)
				}),
			),
			cel.Overload("pick_map_two_strings",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return pickKeysMultiple(args[0], args[1:]...)
				}),
			),
			cel.Overload("pick_map_three_strings",
				[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.StringType, cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return pickKeysMultiple(args[0], args[1:]...)
				}),
			),
		),
	}
}

// mergeMaps merges two maps, with values from m2 overriding m1.
func mergeMaps(m1, m2 ref.Val) ref.Val {
	mapper1, ok1 := m1.(traits.Mapper)
	mapper2, ok2 := m2.(traits.Mapper)
	if !ok1 || !ok2 {
		return types.NewErr("merge requires map arguments")
	}

	result := make(map[string]interface{})

	// Copy all keys from first map
	it1 := mapper1.Iterator()
	for it1.HasNext() == types.True {
		key := it1.Next()
		keyStr := key.(types.String)
		val := mapper1.Get(key)
		result[string(keyStr)] = val.Value()
	}

	// Override/add keys from second map
	it2 := mapper2.Iterator()
	for it2.HasNext() == types.True {
		key := it2.Next()
		keyStr := key.(types.String)
		val := mapper2.Get(key)
		result[string(keyStr)] = val.Value()
	}

	return types.DefaultTypeAdapter.NativeToValue(result)
}

// omitKeys returns a map without the specified key.
func omitKeys(mapVal, keyVal ref.Val) ref.Val {
	mapper, ok := mapVal.(traits.Mapper)
	if !ok {
		return types.NewErr("omit requires a map argument")
	}

	keyToOmit := string(keyVal.(types.String))
	result := make(map[string]interface{})

	it := mapper.Iterator()
	for it.HasNext() == types.True {
		key := it.Next()
		keyStr := string(key.(types.String))
		if keyStr != keyToOmit {
			result[keyStr] = mapper.Get(key).Value()
		}
	}

	return types.DefaultTypeAdapter.NativeToValue(result)
}

// pickKeys returns a map with only the specified key.
func pickKeys(mapVal, keyVal ref.Val) ref.Val {
	mapper, ok := mapVal.(traits.Mapper)
	if !ok {
		return types.NewErr("pick requires a map argument")
	}

	keyToPick := string(keyVal.(types.String))
	result := make(map[string]interface{})

	val := mapper.Get(keyVal)
	if val != nil && val.Type() != types.ErrType {
		result[keyToPick] = val.Value()
	}

	return types.DefaultTypeAdapter.NativeToValue(result)
}

// pickKeysMultiple returns a map with only the specified keys.
func pickKeysMultiple(mapVal ref.Val, keys ...ref.Val) ref.Val {
	mapper, ok := mapVal.(traits.Mapper)
	if !ok {
		return types.NewErr("pick requires a map argument")
	}

	result := make(map[string]interface{})

	for _, keyVal := range keys {
		keyStr := string(keyVal.(types.String))
		val := mapper.Get(keyVal)
		if val != nil && val.Type() != types.ErrType {
			result[keyStr] = val.Value()
		}
	}

	return types.DefaultTypeAdapter.NativeToValue(result)
}

// compare compares two ref.Val values and returns -1, 0, or 1.
func compare(a, b ref.Val) int {
	switch av := a.(type) {
	case types.Int:
		if bv, ok := b.(types.Int); ok {
			if av < bv {
				return -1
			} else if av > bv {
				return 1
			}
			return 0
		}
	case types.Double:
		if bv, ok := b.(types.Double); ok {
			if av < bv {
				return -1
			} else if av > bv {
				return 1
			}
			return 0
		}
	case types.String:
		if bv, ok := b.(types.String); ok {
			as, bs := string(av), string(bv)
			if as < bs {
				return -1
			} else if as > bs {
				return 1
			}
			return 0
		}
	}
	return 0
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
// This is used for evaluating enrich params before making the enrichment call,
// and for evaluating aspect conditions where top-level variables are needed.
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

	// For aspect conditions, we need certain variables at the top level of activation.
	// These are declared in the CEL environment and must be present.
	topLevelVars := []string{"result", "error", "_flow", "_operation", "_target", "_timestamp"}
	for _, key := range topLevelVars {
		if val, ok := input[key]; ok {
			activation[key] = val
		} else {
			// Provide defaults for missing variables to avoid undeclared errors
			switch key {
			case "result":
				activation[key] = map[string]interface{}{}
			case "error":
				activation[key] = ""
			case "_flow", "_operation", "_target":
				activation[key] = ""
			case "_timestamp":
				activation[key] = int64(0)
			}
		}
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
	return t.TransformWithContext(ctx, input, enriched, nil, rules)
}

// TransformWithSteps applies transformation rules with step results available.
func (t *CELTransformer) TransformWithSteps(ctx context.Context, input map[string]interface{}, enriched map[string]interface{}, steps map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
	return t.TransformWithContext(ctx, input, enriched, steps, rules)
}

// TransformWithContext applies transformation rules with all context data available.
func (t *CELTransformer) TransformWithContext(ctx context.Context, input map[string]interface{}, enriched map[string]interface{}, steps map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
	output := make(map[string]interface{})

	// Ensure enriched is not nil
	if enriched == nil {
		enriched = make(map[string]interface{})
	}

	// Ensure steps is not nil
	if steps == nil {
		steps = make(map[string]interface{})
	}

	// Build activation with input, output, enriched, and step data
	activation := map[string]interface{}{
		"input":    input,
		"output":   output,
		"ctx":      make(map[string]interface{}),
		"enriched": enriched,
		"step":     steps,
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

// EvaluateCondition evaluates a boolean CEL expression.
// Used for step conditions and other conditional logic.
func (t *CELTransformer) EvaluateCondition(ctx context.Context, data map[string]interface{}, expr string) (bool, error) {
	prog, err := t.Compile(expr)
	if err != nil {
		return false, err
	}

	// Build activation with all available data
	activation := map[string]interface{}{
		"input":    make(map[string]interface{}),
		"output":   make(map[string]interface{}),
		"ctx":      make(map[string]interface{}),
		"enriched": make(map[string]interface{}),
		"step":     make(map[string]interface{}),
		"result":   make(map[string]interface{}),
		"error":    "",
		// Flow metadata defaults
		"_flow":      "",
		"_operation": "",
		"_target":    "",
		"_timestamp": int64(0),
	}

	// Merge provided data into activation
	for key, val := range data {
		activation[key] = val
	}

	// Evaluate
	result, _, err := prog.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("CEL condition eval error: %w", err)
	}

	// Convert result to boolean
	switch v := result.Value().(type) {
	case bool:
		return v, nil
	default:
		return false, fmt.Errorf("condition expression must return boolean, got %T", result.Value())
	}
}

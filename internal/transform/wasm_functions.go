package transform

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/matutetandil/mycel/internal/functions"
)

// WASMFunction represents a WASM function that can be registered in CEL.
type WASMFunction struct {
	Name     string
	Function functions.Function
}

// createWASMFunctionOption creates a CEL function option for a WASM function.
// WASM functions are variadic (accept any number of dynamic arguments).
func createWASMFunctionOption(fn WASMFunction) cel.EnvOption {
	return cel.Function(fn.Name,
		cel.Overload(fn.Name+"_variadic",
			[]*cel.Type{cel.ListType(cel.DynType)}, // variadic as list
			cel.DynType,
			cel.UnaryBinding(func(args ref.Val) ref.Val {
				// Convert CEL list to Go slice
				goArgs, err := celListToSlice(args)
				if err != nil {
					return types.NewErr("failed to convert args: %v", err)
				}

				// Call WASM function
				result, err := fn.Function.Call(goArgs...)
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}

				// Convert result back to CEL
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
		// Also support direct calls with 0-5 arguments for convenience
		cel.Overload(fn.Name+"_0",
			[]*cel.Type{},
			cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val {
				result, err := fn.Function.Call()
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
		cel.Overload(fn.Name+"_1",
			[]*cel.Type{cel.DynType},
			cel.DynType,
			cel.UnaryBinding(func(arg ref.Val) ref.Val {
				goArg := celToGo(arg)
				result, err := fn.Function.Call(goArg)
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
		cel.Overload(fn.Name+"_2",
			[]*cel.Type{cel.DynType, cel.DynType},
			cel.DynType,
			cel.BinaryBinding(func(arg1, arg2 ref.Val) ref.Val {
				goArg1 := celToGo(arg1)
				goArg2 := celToGo(arg2)
				result, err := fn.Function.Call(goArg1, goArg2)
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
		cel.Overload(fn.Name+"_3",
			[]*cel.Type{cel.DynType, cel.DynType, cel.DynType},
			cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val {
				goArgs := make([]interface{}, len(args))
				for i, arg := range args {
					goArgs[i] = celToGo(arg)
				}
				result, err := fn.Function.Call(goArgs...)
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
		cel.Overload(fn.Name+"_4",
			[]*cel.Type{cel.DynType, cel.DynType, cel.DynType, cel.DynType},
			cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val {
				goArgs := make([]interface{}, len(args))
				for i, arg := range args {
					goArgs[i] = celToGo(arg)
				}
				result, err := fn.Function.Call(goArgs...)
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
		cel.Overload(fn.Name+"_5",
			[]*cel.Type{cel.DynType, cel.DynType, cel.DynType, cel.DynType, cel.DynType},
			cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val {
				goArgs := make([]interface{}, len(args))
				for i, arg := range args {
					goArgs[i] = celToGo(arg)
				}
				result, err := fn.Function.Call(goArgs...)
				if err != nil {
					return types.NewErr("WASM function %s error: %v", fn.Name, err)
				}
				return types.DefaultTypeAdapter.NativeToValue(result)
			}),
		),
	)
}

// celListToSlice converts a CEL list to a Go slice.
func celListToSlice(val ref.Val) ([]interface{}, error) {
	list, ok := val.(interface {
		Size() ref.Val
		Get(ref.Val) ref.Val
	})
	if !ok {
		return nil, fmt.Errorf("expected list, got %T", val)
	}

	sizeVal := list.Size()
	size, ok := sizeVal.(types.Int)
	if !ok {
		return nil, fmt.Errorf("expected int size, got %T", sizeVal)
	}

	result := make([]interface{}, int64(size))
	for i := int64(0); i < int64(size); i++ {
		item := list.Get(types.Int(i))
		result[i] = celToGo(item)
	}

	return result, nil
}

// celToGo converts a CEL value to a Go value.
func celToGo(val ref.Val) interface{} {
	if val == nil || val == types.NullValue {
		return nil
	}

	// Use Value() to get the native Go representation
	return val.Value()
}

// CreateWASMFunctionOptions creates CEL function options from a functions registry.
func CreateWASMFunctionOptions(registry *functions.Registry) []cel.EnvOption {
	if registry == nil {
		return nil
	}

	funcs := registry.GetAllFunctions()
	options := make([]cel.EnvOption, 0, len(funcs))

	for name, fn := range funcs {
		options = append(options, createWASMFunctionOption(WASMFunction{
			Name:     name,
			Function: fn,
		}))
	}

	return options
}

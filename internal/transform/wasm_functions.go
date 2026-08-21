package transform

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/matutetandil/mycel/v3/internal/functions"
)

// WASMFunction represents a WASM function that can be registered in CEL.
type WASMFunction struct {
	Name     string
	Function functions.Function
}

// createWASMFunctionOption creates a CEL function option for a WASM function.
//
// One overload per arity, up to five. There used to be a variadic overload
// taking list(dyn) as well, and dyn matches a list — so it collided with the
// single-argument overload and CEL refused to build the environment at all.
// Every plugin-provided function therefore broke the expression language for
// the whole flow, whether or not anything called it: "overload signature
// collision in function <name>". A call with more than five arguments is now
// refused when the expression is compiled, which is a message about that call
// rather than about everything.
func createWASMFunctionOption(fn WASMFunction) cel.EnvOption {
	return cel.Function(fn.Name,
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

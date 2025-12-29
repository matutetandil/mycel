// Package hcl provides custom HCL functions for Mycel configuration.
package hcl

import (
	"os"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// Functions returns the custom HCL functions available in Mycel configs.
func Functions() map[string]function.Function {
	return map[string]function.Function{
		"env":     EnvFunc,
		"coalesce": CoalesceFunc,
	}
}

// EnvFunc returns the value of an environment variable.
// Usage: env("DB_HOST") or env("DB_HOST", "localhost")
var EnvFunc = function.New(&function.Spec{
	Description: "Returns the value of the specified environment variable",
	Params: []function.Parameter{
		{
			Name:        "name",
			Description: "Name of the environment variable",
			Type:        cty.String,
		},
	},
	VarParam: &function.Parameter{
		Name:        "default",
		Description: "Default value if environment variable is not set",
		Type:        cty.String,
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		envName := args[0].AsString()
		value := os.Getenv(envName)

		// If env var is empty and we have a default, use it
		if value == "" && len(args) > 1 {
			value = args[1].AsString()
		}

		return cty.StringVal(value), nil
	},
})

// CoalesceFunc returns the first non-empty string argument.
// Usage: coalesce(env("DB_HOST"), "localhost")
var CoalesceFunc = function.New(&function.Spec{
	Description: "Returns the first non-empty string from the arguments",
	VarParam: &function.Parameter{
		Name:        "values",
		Description: "Values to check",
		Type:        cty.String,
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		for _, arg := range args {
			if !arg.IsNull() {
				s := arg.AsString()
				if s != "" {
					return cty.StringVal(s), nil
				}
			}
		}
		return cty.StringVal(""), nil
	},
})

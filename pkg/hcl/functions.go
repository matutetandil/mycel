// Package hcl provides custom HCL functions for Mycel configuration.
package hcl

import (
	"encoding/base64"
	"os"
	"path/filepath"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// Functions returns the custom HCL functions available in Mycel configs.
func Functions() map[string]function.Function {
	return map[string]function.Function{
		"env":        EnvFunc,
		"coalesce":   CoalesceFunc,
		"file":       FileFunc,
		"base64decode": Base64DecodeFunc,
		"base64encode": Base64EncodeFunc,
		"abspath":    AbsPathFunc,
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

// FileFunc reads a file and returns its contents as a string.
// Usage: file("./secrets/db-password.txt")
var FileFunc = function.New(&function.Spec{
	Description: "Reads a file and returns its contents",
	Params: []function.Parameter{
		{
			Name:        "path",
			Description: "Path to the file",
			Type:        cty.String,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		path := args[0].AsString()
		content, err := os.ReadFile(path)
		if err != nil {
			return cty.StringVal(""), err
		}
		return cty.StringVal(string(content)), nil
	},
})

// Base64DecodeFunc decodes a base64-encoded string.
// Usage: base64decode("SGVsbG8gV29ybGQ=")
var Base64DecodeFunc = function.New(&function.Spec{
	Description: "Decodes a base64-encoded string",
	Params: []function.Parameter{
		{
			Name:        "encoded",
			Description: "Base64-encoded string",
			Type:        cty.String,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		encoded := args[0].AsString()
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return cty.StringVal(""), err
		}
		return cty.StringVal(string(decoded)), nil
	},
})

// Base64EncodeFunc encodes a string to base64.
// Usage: base64encode("Hello World")
var Base64EncodeFunc = function.New(&function.Spec{
	Description: "Encodes a string to base64",
	Params: []function.Parameter{
		{
			Name:        "value",
			Description: "String to encode",
			Type:        cty.String,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		value := args[0].AsString()
		encoded := base64.StdEncoding.EncodeToString([]byte(value))
		return cty.StringVal(encoded), nil
	},
})

// AbsPathFunc converts a relative path to an absolute path.
// Usage: abspath("./data/db.sqlite")
var AbsPathFunc = function.New(&function.Spec{
	Description: "Converts a relative path to absolute",
	Params: []function.Parameter{
		{
			Name:        "path",
			Description: "Path to convert",
			Type:        cty.String,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		path := args[0].AsString()
		absPath, err := filepath.Abs(path)
		if err != nil {
			return cty.StringVal(path), err
		}
		return cty.StringVal(absPath), nil
	},
})

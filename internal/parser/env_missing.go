package parser

import (
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// collectMissingEnv walks a block body looking for env("NAME") calls whose
// environment variable is unset and that have no default value. Those calls
// evaluate to an empty string, so the connector factory later fails with a
// generic "requires X" error; recording them here lets the runtime name the
// variable that is actually missing.
func collectMissingEnv(body hcl.Body) []connector.MissingEnvVar {
	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil
	}

	seen := make(map[string]bool)
	var missing []connector.MissingEnvVar
	walkBodyForEnv(syntaxBody, "", seen, &missing)

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Attr != missing[j].Attr {
			return missing[i].Attr < missing[j].Attr
		}
		return missing[i].Name < missing[j].Name
	})
	return missing
}

// walkBodyForEnv recurses into nested blocks, prefixing attribute paths.
func walkBodyForEnv(body *hclsyntax.Body, prefix string, seen map[string]bool, missing *[]connector.MissingEnvVar) {
	for name, attr := range body.Attributes {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		for _, envName := range unresolvedEnvCalls(attr.Expr) {
			key := path + "\x00" + envName
			if seen[key] {
				continue
			}
			seen[key] = true
			*missing = append(*missing, connector.MissingEnvVar{Name: envName, Attr: path})
		}
	}

	for _, block := range body.Blocks {
		path := block.Type
		if prefix != "" {
			path = prefix + "." + block.Type
		}
		walkBodyForEnv(block.Body, path, seen, missing)
	}
}

// unresolvedEnvCalls returns the names of env() calls in expr that have no
// default argument and whose variable is not set in the process environment.
func unresolvedEnvCalls(expr hclsyntax.Expression) []string {
	var names []string

	hclsyntax.VisitAll(expr, func(node hclsyntax.Node) hcl.Diagnostics {
		call, ok := node.(*hclsyntax.FunctionCallExpr)
		if !ok || call.Name != "env" || len(call.Args) != 1 {
			return nil
		}

		// The variable name must be a literal to be reportable.
		val, diags := call.Args[0].Value(nil)
		if diags.HasErrors() || val.IsNull() || val.Type() != cty.String {
			return nil
		}

		name := val.AsString()
		if name != "" && os.Getenv(name) == "" {
			names = append(names, name)
		}
		return nil
	})

	return names
}

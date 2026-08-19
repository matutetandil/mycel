package parser

import (
	"fmt"
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

		name := stringOrEmpty(val)
		if name != "" && os.Getenv(name) == "" {
			names = append(names, name)
		}
		return nil
	})

	return names
}

// UnsetEnvVar is an env("NAME") call with no default whose variable is not set,
// anywhere in the configuration.
//
// The connector version of this was written first because a connector that
// cannot start reports a generic "requires X" and the variable behind it was
// the missing piece. But every block reads env() the same way and none of the
// others said anything — so `auth { jwt { secret = env("JWT_SECRET") } }` with
// the variable unset produced a service refusing to start with "JWT secret is
// required", naming neither the variable nor the file.
type UnsetEnvVar struct {
	// Block is where it was written, as the reader would name it: `auth`,
	// `connector "api"`, `service`.
	Block string
	// Attr is the attribute path inside that block, such as `jwt.secret`.
	Attr string
	// Name is the environment variable.
	Name string
}

// describeBlock names a block the way somebody reading the file would.
func describeBlock(block *hcl.Block) string {
	if len(block.Labels) > 0 {
		return fmt.Sprintf("%s %q", block.Type, block.Labels[0])
	}
	return block.Type
}

// CollectUnsetEnv walks a root block for env() calls that resolve to nothing.
func CollectUnsetEnv(blockLabel string, body hcl.Body) []UnsetEnvVar {
	found := collectMissingEnv(body)
	if len(found) == 0 {
		return nil
	}

	out := make([]UnsetEnvVar, 0, len(found))
	for _, m := range found {
		out = append(out, UnsetEnvVar{Block: blockLabel, Attr: m.Attr, Name: m.Name})
	}
	return out
}

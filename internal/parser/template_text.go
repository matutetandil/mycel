package parser

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// templateText reads an attribute that holds a template rather than a value.
//
// A cache key, an invalidation key, a pattern: the runtime replaces ${input.id}
// in them when a request arrives, so they have to survive parsing as written.
// HCL sees the same ${...}, tries to resolve `input` as a variable it does not
// have, and refuses the line — which is why these are read from the source text
// instead of being evaluated.
//
// Reading the source text brings the quotes with it. An aspect's cache key has
// done exactly that since it was written, so every key it produced carried a
// pair of stray quote characters into the cache — consistently, so it worked,
// which is why nobody noticed.
func templateText(expr hcl.Expression) string {
	return unquote(extractExpressionText(expr))
}

// templateList reads a list of templates, element by element.
//
// Taking the source text of the whole expression would give one string holding
// the brackets and every element inside it, which is not a list of anything.
func templateList(expr hcl.Expression) []string {
	if tuple, ok := expr.(*hclsyntax.TupleConsExpr); ok {
		out := make([]string, 0, len(tuple.Exprs))
		for _, item := range tuple.Exprs {
			if text := templateText(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	}

	// A single template where a list was expected is a list of one, the same
	// reading stringList gives a single name.
	if text := templateText(expr); text != "" {
		return []string{text}
	}
	return nil
}

// unquote strips the pair of quotes the source text carries.
func unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return s
}

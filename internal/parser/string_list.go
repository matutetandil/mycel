package parser

import (
	"github.com/zclconf/go-cty/cty"
)

// stringList reads an attribute that holds a list of names.
//
// A single value counts as a list of one. `invalidate_on = "update_user"`
// instead of `["update_user"]` is the mistake somebody makes once per language
// they learn, and every one of these attributes used to be read behind a bare
// `if val.Type().IsListType()` whose else branch did nothing at all: the line
// was accepted, the value thrown away, and the cache invalidated nothing, the
// rule required no roles, the aspect matched nothing. The service started and
// did less than it was told to, silently.
//
// Only the aspect's `on` had the single-value branch, which is why it worked
// and its seven neighbours did not.
func stringList(val cty.Value) []string {
	if val.IsNull() {
		return nil
	}

	t := val.Type()
	if t.IsListType() || t.IsTupleType() || t.IsSetType() {
		var out []string
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			out = append(out, stringOrEmpty(v))
		}
		return out
	}

	// A single value, which is a list of one. Numbers and booleans are read
	// the way a person wrote them rather than refused, matching how every
	// other attribute in this parser is read.
	if s := stringOrEmpty(val); s != "" {
		return []string{s}
	}
	return nil
}

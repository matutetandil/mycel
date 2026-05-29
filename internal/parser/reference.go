package parser

import "strings"

// parseRefName strips the "<kind>." prefix from a reference string used in
// reusable-block references like `use = "dedupe.standard"` or
// `cache = "cache.products"`. If the prefix is absent the input is returned
// unchanged so callers can keep accepting bare names for backward compatibility.
func parseRefName(kind, ref string) string {
	return strings.TrimPrefix(ref, kind+".")
}

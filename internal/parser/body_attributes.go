package parser

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// bodyAttributes reads a body's attributes while tolerating blocks alongside
// them.
//
// hcl.Body.JustAttributes is all-or-nothing on native syntax: one block
// anywhere in the body and it returns "Blocks are not allowed here", even for a
// body already narrowed by PartialContent. Blocks a caller has deliberately
// consumed still count, so a block type the parser supports becomes impossible
// to write.
//
// Falls back to JustAttributes for bodies that are not native syntax — JSON
// configuration, and the synthetic bodies used in tests — where the behaviour
// is the documented one and there is nothing to work around.
func bodyAttributes(body hcl.Body) (hcl.Attributes, error) {
	if syn, ok := body.(*hclsyntax.Body); ok {
		attrs := make(hcl.Attributes, len(syn.Attributes))
		for name, attr := range syn.Attributes {
			attrs[name] = attr.AsHCLAttribute()
		}
		return attrs, nil
	}

	attrs, diags := body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	return attrs, nil
}

// attributeOrder returns the attribute names in the order they appear in the
// source file.
//
// hcl.Attributes is a map, so a block that is written top to bottom arrives
// unordered. Blocks whose attributes are CEL expressions evaluated in sequence
// — transform, response, error_response body — need that order back, because a
// later expression may reference an earlier field through `output`. Each
// attribute carries the byte offset where it was written, which is all the
// ordering needs.
func attributeOrder(attrs hcl.Attributes) []string {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := attrs[names[i]].Range.Start.Byte, attrs[names[j]].Range.Start.Byte
		if a != b {
			return a < b
		}
		// Synthetic bodies (tests, JSON) may carry no ranges at all; falling
		// back to the name keeps the result stable instead of arbitrary.
		return names[i] < names[j]
	})
	return names
}

package parser

import (
	"fmt"

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

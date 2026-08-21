package parser

import "sort"

// What the language contains, by name, for anything that needs to count it.
//
// docs/llms.txt states how many blocks a flow can hold, how many attributes,
// and how many inline blocks can be named and reused. Those numbers are read
// by an assistant answering questions about Mycel, so they are counted from
// here rather than kept in step by hand.

// FlowBlockNames lists the blocks a flow may contain.
func FlowBlockNames() []string {
	body := flowBodySchema()
	names := make([]string, 0, len(body.Blocks))
	for _, b := range body.Blocks {
		names = append(names, b.Type)
	}
	sort.Strings(names)
	return names
}

// FlowAttributeNames lists the attributes a flow may carry.
func FlowAttributeNames() []string {
	body := flowBodySchema()
	names := make([]string, 0, len(body.Attributes))
	for _, a := range body.Attributes {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}

// ReusableKindNames lists the inline blocks that can be declared once at the
// top level and referenced with use = "<kind>.<name>".
func ReusableKindNames() []string {
	names := make([]string, 0, len(reusableKinds))
	for _, k := range reusableKinds {
		names = append(names, k.typeName)
	}
	sort.Strings(names)
	return names
}

// ConnectorAttributeNames returns the attribute names a connector block
// accepts.
//
// Exported so the documentation can be checked against it. The parser is the
// gatekeeper: a name it does not accept is refused however well the connector
// would have handled it, which is what the tcp page's `address` was — read by
// the connector's own code, refused before it ever got there, and shown in a
// table for anyone to copy.
func ConnectorAttributeNames() []string {
	body := connectorBodySchema()
	names := make([]string, 0, len(body.Attributes)+len(body.Blocks))
	for _, attr := range body.Attributes {
		names = append(names, attr.Name)
	}
	for _, block := range body.Blocks {
		names = append(names, block.Type)
	}
	return names
}

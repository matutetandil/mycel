package graphql

import "testing"

// Introspection publishes a map of everything the service can do, so turning it
// off has to actually turn it off. The attribute was accepted by the parser and
// read by nothing, which meant asking for it left it on.

func TestQueryUsesIntrospection(t *testing.T) {
	for _, q := range []string{
		`{ __schema { types { name } } }`,
		`query { __type(name: "User") { fields { name } } }`,
		`{  __SCHEMA { types { name } } }`,
		"{\n  __schema {\n    queryType { name }\n  }\n}",
	} {
		if !queryUsesIntrospection(q) {
			t.Errorf("introspection not detected in %q", q)
		}
	}

	for _, q := range []string{
		`{ users { id name } }`,
		`mutation { createUser(name: "x") { id } }`,
		// A field whose name merely ends in the text, and a string argument
		// carrying it, are ordinary queries.
		`{ search(term: "__schema") { id } }`,
		`{ my__schema { id } }`,
	} {
		if queryUsesIntrospection(q) {
			t.Errorf("ordinary query treated as introspection: %q", q)
		}
	}
}

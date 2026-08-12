package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestTheRefusalIsShapedLikeAGraphQLError(t *testing.T) {
	// A GraphQL client reads errors from the body, not from the status line, so
	// a refusal arrives as a 200 carrying an errors array. A bare 403 would
	// surface in most clients as a transport failure with no explanation.
	rec := httptest.NewRecorder()

	writeGraphQLError(rec, "introspection is disabled", http.StatusOK)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if len(body.Errors) != 1 || body.Errors[0].Message != "introspection is disabled" {
		t.Errorf("body = %s", rec.Body.String())
	}
}

package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
)

// SubscriptionQuery builds the document a subscription client sends for one
// field, with the selection a flow asked for.
//
// Without a selection the document is `subscription { field }`, which only a
// field typed as a scalar or JSON can answer. A field typed as an object —
// what any server with a real schema declares — has to be asked which of its
// fields to send, and the server refuses the bare form: "Field "priceChanged"
// of type "PriceChange" must have a sub selection". Nothing could say it, so a
// subscription to a typed server was refused on every attempt and retried for
// ever.
//
// The selection is written as GraphQL, with or without its braces:
// `{ sku newPrice }` or `sku newPrice`. Nested objects are written the same way
// a client writes them. The result is parsed, so a selection that would not
// form one subscription to that one field is refused here, where validate can
// report it, rather than by the remote server on the first connect.
func SubscriptionQuery(field, selection string) (string, error) {
	selection = strings.TrimSpace(selection)
	query := fmt.Sprintf("subscription { %s }", field)
	if selection != "" {
		if !strings.HasPrefix(selection, "{") {
			selection = "{ " + selection + " }"
		}
		query = fmt.Sprintf("subscription { %s %s }", field, selection)
	}

	doc, err := parser.Parse(parser.ParseParams{Source: query})
	if err != nil {
		return "", fmt.Errorf("selection for %q is not valid GraphQL: %w", field, err)
	}
	if !isOneSubscriptionTo(doc, field) {
		return "", fmt.Errorf("selection for %q must be the fields of %q only, not another operation or field", field, field)
	}
	return query, nil
}

// isOneSubscriptionTo reports whether doc is a single subscription selecting
// exactly the one field — a selection like `{ a } } query { b` parses, but as
// a second operation.
func isOneSubscriptionTo(doc *ast.Document, field string) bool {
	if len(doc.Definitions) != 1 {
		return false
	}
	op, ok := doc.Definitions[0].(*ast.OperationDefinition)
	if !ok || op.Operation != ast.OperationTypeSubscription || op.SelectionSet == nil {
		return false
	}
	if len(op.SelectionSet.Selections) != 1 {
		return false
	}
	f, ok := op.SelectionSet.Selections[0].(*ast.Field)
	return ok && f.Name != nil && f.Name.Value == field
}

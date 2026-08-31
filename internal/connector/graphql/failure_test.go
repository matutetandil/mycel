package graphql

import (
	"errors"
	"strings"
	"testing"

	"github.com/graphql-go/graphql/gqlerrors"
)

func TestAProductionResolverFailureIsNotDescribed(t *testing.T) {
	// A resolver error carries the path to the field it happened on. That is
	// a failure inside the service, and its text is the inside of the service.
	errs := []gqlerrors.FormattedError{{
		Message: "response transform error: failed to evaluate expression for 'count': " +
			"no such table: internal_billing_table",
		Path: []interface{}{"orders"},
	}}

	withheld := withheldFailures(errs, "production")

	if strings.Contains(withheld[0].Message, "internal_billing_table") {
		t.Errorf("the name of an internal table was published: %q", withheld[0].Message)
	}
	// The shape of the response is not touched: a client reads the path to
	// know which field it lost.
	if len(withheld[0].Path) != 1 || withheld[0].Path[0] != "orders" {
		t.Errorf("the path of the error was lost: %v", withheld[0].Path)
	}
}

func TestAMalformedQueryIsStillExplainedInProduction(t *testing.T) {
	// A query naming a field that does not exist failed before any resolver
	// ran. The client wrote it, the client is the only one who can fix it, and
	// that message is most of what a GraphQL endpoint is worth to whoever is
	// writing against it. Errors from parsing and validation carry no path,
	// which is how they are told apart.
	errs := []gqlerrors.FormattedError{{
		Message: `Cannot query field "nosuchfield" on type "Query".`,
	}}

	withheld := withheldFailures(errs, "production")

	if withheld[0].Message != errs[0].Message {
		t.Errorf("a caller was not told its query was malformed: %q", withheld[0].Message)
	}
}

func TestOutsideProductionTheResponseIsUntouched(t *testing.T) {
	errs := []gqlerrors.FormattedError{{
		Message: "no such table: internal_billing_table",
		Path:    []interface{}{"orders"},
	}}

	if got := withheldFailures(errs, "development"); got[0].Message != errs[0].Message {
		t.Errorf("a developer was not told what actually failed: %q", got[0].Message)
	}
}

func TestAValidationFailureInsideAFlowIsStillQuoted(t *testing.T) {
	// Classification is by type all the way down, so a request that failed the
	// contract a flow declares is quoted back to the caller who caused it,
	// even though it surfaced as a resolver error with a path.
	wrapped := gqlerrors.NewFormattedError("ignored")
	if resolverError(wrapped) == nil {
		t.Fatal("the original error could not be recovered from a formatted one")
	}

	// And an error graphql-go wrapped twice is still reached.
	inner := errors.New("validation error on 'email': field is required")
	formatted := gqlerrors.FormatError(&gqlerrors.Error{
		Message:       inner.Error(),
		OriginalError: inner,
		Path:          []interface{}{"createUser"},
	})
	if got := resolverError(formatted); got != inner {
		t.Errorf("the error a resolver returned was not recovered: %v", got)
	}
}

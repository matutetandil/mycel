package graphql

import (
	"errors"

	"github.com/graphql-go/graphql/gqlerrors"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// withheldFailures rewrites the errors of a GraphQL response so that a
// production caller is not handed the inside of the service.
//
// The REST connector was the only server that asked what environment it was
// running in; this one encoded whatever graphql-go put in the response, so a
// flow that failed while shaping its answer published the reason — the
// expression that broke, the key that was missing, and, for a failure further
// down, the driver text underneath it.
//
// Not every error here is ours. A query that names a field that does not exist
// or passes an argument of the wrong type failed before any resolver ran, and
// that message is the whole value of a GraphQL endpoint to the client writing
// against it: it must survive. The two are told apart by where they happened —
// an error raised resolving a field carries the path to that field, and one
// raised while parsing or validating the document carries none.
func withheldFailures(errs []gqlerrors.FormattedError, environment string) []gqlerrors.FormattedError {
	if len(errs) == 0 || !connector.IsProduction(environment) {
		return errs
	}

	withheld := make([]gqlerrors.FormattedError, len(errs))
	for i, formatted := range errs {
		withheld[i] = formatted
		if len(formatted.Path) == 0 {
			// The document itself was refused. The caller wrote it and is
			// the only one who can fix it.
			continue
		}
		withheld[i].Message = connector.FailureMessage(
			resolverError(formatted), environment, "internal error")
	}
	return withheld
}

// resolverError digs out the error a resolver actually returned, which
// graphql-go wraps twice before it reaches the response. Classification is by
// type, so a validation failure raised inside a flow is still quoted back to
// the caller who caused it.
func resolverError(formatted gqlerrors.FormattedError) error {
	original := formatted.OriginalError()
	if original == nil {
		return errors.New(formatted.Message)
	}

	var wrapped *gqlerrors.Error
	if errors.As(original, &wrapped) && wrapped.OriginalError != nil {
		return wrapped.OriginalError
	}
	return original
}

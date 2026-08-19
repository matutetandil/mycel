package runtime

import (
	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// ValidateAll runs every configuration check and returns everything wrong.
//
// One list, used by `mycel validate` and by the runtime, so the two cannot come
// to disagree about what is checked — a configuration that passes validate and
// then refuses to start is worse than either outcome on its own.
//
// Every check is run: they grew one at a time, each returning as soon as it
// found something, so a configuration wrong in three ways took three runs to
// find out. That is the experience each check avoids inside itself, and they
// recreated it between them.
func ValidateAll(config *parser.Configuration, reg *schema.Registry) []error {
	var errs []error
	for _, check := range Checks(config, reg) {
		errs = append(errs, check.Errors...)
	}
	return errs
}

// Check is one named group of problems, so a caller can say how many of what
// kind it found.
type Check struct {
	Kind   string
	Errors []error
}

// Checks runs everything and returns the results grouped by kind.
func Checks(config *parser.Configuration, reg *schema.Registry) []Check {
	return []Check{
		// Every flow's "from" block against its source connector's schema, so
		// a missing required parameter fails here rather than surfacing later
		// as a confusing runtime error.
		{"flow", ValidateFlowSchemas(config, reg)},
		// A duration that cannot be read is discarded at the point of use, so
		// a cache that meant to last five minutes lasts however long the
		// connector defaults to.
		{"duration", ValidateFlowDurations(config)},
		// Three words are implemented for a step's on_error and anything else
		// fails the flow, which is the opposite of what most of them read as.
		{"step", ValidateStepErrorHandling(config)},
		// A type a flow validates against, and a flow an aspect invokes: the
		// first is a 500 on the first request, the second a warning per
		// message and an aspect that does nothing.
		{"type reference", ValidateTypeReferences(config)},
		{"aspect flow reference", ValidateAspectFlowReferences(config)},
		// A validator a type names but nothing declares is not a failure at
		// run time: the rule is skipped, so the field goes unvalidated.
		{"validator reference", ValidateValidatorReferences(config)},
		// Names repeated inside a flow, where something is keyed by them: the
		// second silently overwrites the first.
		{"duplicate name", ValidateUniqueInnerNames(config)},
		// A connector named by a block other than from/to was checked by
		// nobody: depending on the block it was refused, or failed on the
		// first request, or silently did nothing at all for ever.
		{"connector reference", ValidateConnectorReferences(config)},
		// A hook naming a flow that does not exist would otherwise surface as
		// a line in a log during whatever the hook was meant to catch.
		{"auth hook", ValidateAuthHooks(config)},
		// And each connector's settings against the words that connector
		// accepts, so a misspelt auth type is caught here rather than by
		// whoever wonders why every request comes back unauthorised.
		{"connector", ValidateConnectorSchemas(config, reg)},
	}
}

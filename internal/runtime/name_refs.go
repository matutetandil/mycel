package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/parser"
)

// The last two namespaces a configuration can point into by name.
//
// A type named by `validate { input = "user" }` is looked up when a request
// arrives, so a typo is a 500 on the first one through the door — production
// rather than deploy. A flow named by an aspect's action is worse: an `after`
// aspect whose action fails is logged at warning level and the flow carries
// on, so an audit aspect pointing at a misspelt flow writes nothing, for ever,
// while producing a line per message that nobody reads.
//
// Both are typos, and a typo should stop a deployment.

// ValidateTypeReferences reports every type named by a flow that no type block
// declares.
func ValidateTypeReferences(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	declared := make(map[string]bool, len(config.Types))
	for _, t := range config.Types {
		if t != nil {
			declared[t.Name] = true
		}
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil || f.Validate == nil {
			continue
		}
		for what, name := range map[string]string{
			"validate.input":  f.Validate.Input,
			"validate.output": f.Validate.Output,
		} {
			if name == "" || declared[name] {
				continue
			}
			errs = append(errs, fmt.Errorf("flow %q: %s names type %q, which no type block declares%s",
				f.Name, what, name, availableList("type", declared)))
		}
	}

	// Sorted, because the map above makes the order of the two arbitrary and a
	// list of errors that reads differently each run is hard to work from.
	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errs
}

// ValidateAspectFlowReferences reports every flow named by an aspect that no
// flow block declares.
func ValidateAspectFlowReferences(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	declared := make(map[string]bool, len(config.Flows))
	for _, f := range config.Flows {
		if f != nil {
			declared[f.Name] = true
		}
	}

	var errs []error
	for _, a := range config.Aspects {
		if a == nil || a.Action == nil || a.Action.Flow == "" {
			continue
		}
		if declared[a.Action.Flow] {
			continue
		}
		errs = append(errs, fmt.Errorf("aspect %q: action names flow %q, which no flow block declares%s",
			a.Name, a.Action.Flow, availableList("flow", declared)))
	}
	return errs
}

// availableList offers what was declared, which is usually where the intended
// name is.
func availableList(kind string, declared map[string]bool) string {
	if len(declared) == 0 {
		return fmt.Sprintf(" (this configuration declares no %ss)", kind)
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf(" (declared: %s)", strings.Join(names, ", "))
}

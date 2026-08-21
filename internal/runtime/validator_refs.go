package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/parser"
)

// Validators a type names.
//
// A field carrying `validator = "corporate_email"` is checked against the
// registry when the flow runs, and a name that is not there is skipped:
// `resolveValidatorRefs` does `if !ok { continue }`. So a typo does not fail,
// it turns the rule off — and a validator's whole job is refusing input that
// should not get in, which makes this the worst place in the language for a
// name to go unchecked.
func ValidateValidatorReferences(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	// A plugin can provide validators, and what a plugin provides is read when
	// the plugin is loaded rather than when the configuration is parsed — so a
	// name this cannot see may still exist. Where there are plugins, an
	// unknown name is left alone: a check that refuses a working configuration
	// is worse than one that misses a typo, and the integration suite here
	// runs exactly that shape.
	if len(config.Plugins) > 0 {
		return nil
	}

	declared := make(map[string]bool, len(config.Validators))
	for _, v := range config.Validators {
		if v != nil {
			declared[v.Name] = true
		}
	}

	var errs []error
	for _, t := range config.Types {
		if t == nil {
			continue
		}
		for _, field := range t.Fields {
			if field.ValidatorRef == "" || declared[field.ValidatorRef] {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"type %q: field %q names validator %q, which no validator block declares%s",
				t.Name, field.Name, field.ValidatorRef, declaredValidators(declared)))
		}
	}
	return errs
}

func declaredValidators(declared map[string]bool) string {
	if len(declared) == 0 {
		return " (this configuration declares none)"
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf(" (declared: %s)", strings.Join(names, ", "))
}

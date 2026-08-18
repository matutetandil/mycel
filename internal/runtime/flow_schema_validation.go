package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v2/internal/aspect"
	"github.com/matutetandil/mycel/v2/internal/auth"
	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// ValidateFlowSchemas checks every flow's "from" block against the source schema
// declared by its connector. Connectors describe which flow parameters they need
// (see ConnectorSchemaProvider.SourceSchema); until now that contract was only
// consumed by the IDE engine, so a REST flow missing its "operation" reached the
// runtime and failed later with an unrelated error — or silently registered a
// handler for the empty operation.
//
// Only attributes marked Required are enforced. Connectors that default a
// parameter (message queues default "operation" to the catch-all "*") declare it
// optional and are left alone.
//
// Returns one error per offending flow, sorted by flow name for stable output.
// Connectors with no registered schema (plugins, custom types) are skipped.
func ValidateFlowSchemas(config *parser.Configuration, reg *schema.Registry) []error {
	if config == nil || reg == nil {
		return nil
	}

	// Index connectors by name so each flow can resolve its source type/driver.
	byName := make(map[string]*connectorRef, len(config.Connectors))
	for _, c := range config.Connectors {
		if c == nil {
			continue
		}
		byName[c.Name] = &connectorRef{Type: c.Type, Driver: c.Driver}
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil || f.From == nil || f.From.Connector == "" {
			continue
		}

		ref, ok := byName[f.From.Connector]
		if !ok {
			// Unknown connector: registerFlows reports this with a better message.
			continue
		}

		provider := reg.Lookup(ref.Type, ref.Driver)
		if provider == nil {
			continue
		}

		src := provider.SourceSchema()
		if src == nil {
			continue
		}

		var missing []string
		for _, attr := range src.Attrs {
			if !attr.Required {
				continue
			}
			if !hasParam(f.From.ConnectorParams, attr.Name) {
				missing = append(missing, attr.Name)
			}
		}
		if len(missing) == 0 {
			continue
		}

		sort.Strings(missing)
		errs = append(errs, fmt.Errorf(
			"flow %q: from block is missing %s required by connector %q (%s)",
			f.Name, quoteList(missing), f.From.Connector, describeType(ref),
		))
	}

	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errs
}

// connectorRef is the minimal connector identity needed for a schema lookup.
type connectorRef struct {
	Type   string
	Driver string
}

// describeType renders "type" or "type/driver" for error messages.
func describeType(ref *connectorRef) string {
	if ref.Driver != "" {
		return ref.Type + "/" + ref.Driver
	}
	return ref.Type
}

// hasParam reports whether params holds a non-empty value for name.
// A present-but-blank attribute (operation = "") counts as missing: it is a
// typo, not a deliberate catch-all.
func hasParam(params map[string]interface{}, name string) bool {
	v, ok := params[name]
	if !ok || v == nil {
		return false
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	return true
}

// quoteList renders attribute names as `"a"` or `"a", "b"`.
func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	if len(quoted) == 1 {
		return "attribute " + quoted[0]
	}
	return "attributes " + strings.Join(quoted, ", ")
}

// ValidateAspects checks aspects the same way startup does.
//
// runtime.New registers every aspect and fails if one is malformed — an action
// naming neither a connector nor a flow, for instance. `mycel validate` builds
// no runtime, so it never reached that check: a config could pass validate and
// then refuse to start, which is the gap validate exists to close.
//
// Deliberately calls the same registry the runtime uses rather than
// reimplementing the rules, so the two cannot drift.
func ValidateAspects(config *parser.Configuration) error {
	if config == nil || len(config.Aspects) == 0 {
		return nil
	}
	return aspect.NewRegistry().RegisterAll(config.Aspects)
}

// ValidateAuthHooks checks that every flow an auth hook names exists.
//
// A hook is invoked at a moment nobody is watching — somebody signing in, an
// account being registered — so a misspelled flow name would otherwise surface
// as a line in a log during whatever the hook was meant to prevent. Checked
// where the flows themselves are, so `mycel validate` refuses it before a
// deployment rather than after.
func ValidateAuthHooks(config *parser.Configuration) []error {
	if config == nil || config.Auth == nil || config.Auth.Hooks == nil {
		return nil
	}

	known := make(map[string]bool, len(config.Flows))
	for _, f := range config.Flows {
		if f != nil {
			known[f.Name] = true
		}
	}

	hooks := config.Auth.Hooks
	named := []struct {
		event string
		hook  *auth.HookConfig
	}{
		{"before_login", hooks.BeforeLogin},
		{"after_login", hooks.AfterLogin},
		{"after_register", hooks.AfterRegister},
		{"on_failed_login", hooks.OnFailedLogin},
		{"on_suspicious_activity", hooks.OnSuspiciousActivity},
		{"before_password_change", hooks.BeforePasswordChange},
		{"after_password_change", hooks.AfterPasswordChange},
	}

	var errs []error
	for _, n := range named {
		if n.hook == nil || n.hook.Flow == "" {
			continue
		}
		if !known[n.hook.Flow] {
			errs = append(errs, fmt.Errorf("auth hook %s names flow %q, which no flow block declares",
				n.event, n.hook.Flow))
		}
	}
	return errs
}

package runtime

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/pkg/schema"
)

// catchAllPatterns accept every delivery. "*" is Mycel's own default; "#" is
// the AMQP topic wildcard, which users reach for by habit.
var catchAllPatterns = map[string]bool{"*": true, "#": true, "": true}

// reportDispatch explains, at startup, which deliveries each flow on a
// subscription source will actually receive.
//
// On a message queue source, `operation` reads like an operation *name* but is
// a subscription *pattern*: a second, in-process filter applied after the
// broker has already decided what lands in the queue. A value matching nothing
// silently discards every delivery — two engineers on the same project hit
// this independently in the same week, which is a design smell rather than
// user error. Spelling it out before the first message arrives is the cheapest
// place to catch it.
//
// Only sources whose connector declares `operation` optional are reported.
// That is exactly the set where it means "narrow what I receive" (message
// queues, MQTT, CDC, WebSocket, file watch); where it is required it addresses
// a specific endpoint and there is nothing to warn about.
func (r *Runtime) reportDispatch(logger *slog.Logger, reg *schema.Registry) {
	if logger == nil || r.config == nil {
		return
	}

	byName := make(map[string]*connectorRef, len(r.config.Connectors))
	for _, c := range r.config.Connectors {
		if c != nil {
			byName[c.Name] = &connectorRef{Type: c.Type, Driver: c.Driver}
		}
	}

	// Group by connector so one line per flow sits under a shared subject.
	perConnector := map[string][]dispatchBinding{}

	for _, f := range r.config.Flows {
		if f == nil || f.From == nil || f.From.Connector == "" {
			continue
		}
		ref, ok := byName[f.From.Connector]
		if !ok || !isSubscriptionSource(reg, ref) {
			continue
		}
		perConnector[f.From.Connector] = append(perConnector[f.From.Connector],
			dispatchBinding{flow: f.Name, pattern: f.From.GetOperation()})
	}

	names := make([]string, 0, len(perConnector))
	for name := range perConnector {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		bindings := perConnector[name]
		sort.Slice(bindings, func(i, j int) bool { return bindings[i].flow < bindings[j].flow })

		anyCatchAll := false
		for _, b := range bindings {
			if catchAllPatterns[b.pattern] {
				anyCatchAll = true
			}
		}

		for _, b := range bindings {
			if catchAllPatterns[b.pattern] {
				logger.Info("dispatch: flow accepts every message",
					"connector", name,
					"flow", b.flow,
					"operation", "(catch-all)",
				)
				continue
			}
			logger.Info("dispatch: flow only accepts matching messages",
				"connector", name,
				"flow", b.flow,
				"operation", b.pattern,
				"meaning", fmt.Sprintf("only deliveries whose key matches %q reach this flow", b.pattern),
			)
		}

		// The dangerous shape: every flow on the connector is narrowed, so a
		// delivery matching none is dropped with nothing to catch it. With a
		// catch-all present there is always a handler, so this cannot happen.
		if !anyCatchAll {
			logger.Warn("dispatch: messages matching no pattern will be DROPPED",
				"connector", name,
				"patterns", patternList(bindings),
				"hint", "on a message queue source `operation` is a subscription pattern, not an operation name; omit it to accept every message",
			)
		}
	}
}

// dispatchBinding pairs a flow with the pattern it registered.
type dispatchBinding struct{ flow, pattern string }

// patternList renders the declared patterns for the warning.
func patternList(bindings []dispatchBinding) string {
	seen := map[string]bool{}
	var out []string
	for _, b := range bindings {
		if !seen[b.pattern] {
			seen[b.pattern] = true
			out = append(out, fmt.Sprintf("%q", b.pattern))
		}
	}
	return strings.Join(out, ", ")
}

// isSubscriptionSource reports whether this connector treats `operation` as a
// pattern that narrows what it receives, rather than as an endpoint it must
// address. The connector's own schema is the authority: it declares operation
// optional exactly when it defaults to the catch-all.
func isSubscriptionSource(reg *schema.Registry, ref *connectorRef) bool {
	if reg == nil || ref == nil {
		return false
	}
	provider := reg.Lookup(ref.Type, ref.Driver)
	if provider == nil {
		return false
	}
	src := provider.SourceSchema()
	if src == nil {
		return false
	}
	for _, attr := range src.Attrs {
		if attr.Name == "operation" {
			return !attr.Required
		}
	}
	return false
}

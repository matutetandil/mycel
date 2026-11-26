package runtime

import (
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/connector/graphql"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// ValidateSubscriptionSelections checks the `selection` of each flow fed by a
// remote GraphQL subscription.
//
// A selection that does not form one subscription to its field would be
// refused by the remote server on every connect and retried for ever, with
// the service reporting itself healthy. And the attribute does something in
// exactly one place — a Subscription.* source on a GraphQL client connector —
// so written anywhere else it is a setting nothing reads, which is refused
// rather than accepted and ignored.
func ValidateSubscriptionSelections(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	clients := make(map[string]bool)
	for _, c := range config.Connectors {
		if c != nil && c.Type == "graphql" && c.Driver == "client" {
			clients[c.Name] = true
		}
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil || f.From == nil {
			continue
		}
		selection := f.From.GetSelection()
		if selection == "" {
			continue
		}
		operation := f.From.GetOperation()
		if !clients[f.From.Connector] || !strings.HasPrefix(operation, "Subscription.") {
			errs = append(errs, fmt.Errorf("flow %q: `selection` only applies to a Subscription.* source on a GraphQL client connector (driver = \"client\"); here nothing would read it", f.Name))
			continue
		}
		if _, err := graphql.SubscriptionQuery(strings.TrimPrefix(operation, "Subscription."), selection); err != nil {
			errs = append(errs, fmt.Errorf("flow %q: %w", f.Name, err))
		}
	}
	return errs
}

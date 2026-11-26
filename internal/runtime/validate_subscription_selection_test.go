package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

func selectionConfig(connType, driver, operation, selection string) *parser.Configuration {
	return &parser.Configuration{
		Connectors: []*connector.Config{{Name: "src", Type: connType, Driver: driver}},
		Flows: []*flow.Config{{
			Name: "track",
			From: &flow.FromConfig{
				Connector:       "src",
				ConnectorParams: map[string]interface{}{"operation": operation, "selection": selection},
			},
		}},
	}
}

func TestASubscriptionSelectionThatFormsOneSubscriptionIsAccepted(t *testing.T) {
	cfg := selectionConfig("graphql", "client", "Subscription.priceChanged", "{ sku newPrice }")
	if errs := ValidateSubscriptionSelections(cfg); len(errs) != 0 {
		t.Fatalf("a valid selection was refused: %v", errs)
	}
}

func TestASubscriptionSelectionThatDoesNotParseIsRefused(t *testing.T) {
	cfg := selectionConfig("graphql", "client", "Subscription.priceChanged", "{ sku newPrice")
	errs := ValidateSubscriptionSelections(cfg)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "track") {
		t.Fatalf("want one error naming the flow, got %v", errs)
	}
}

// selection is read in one place only; written anywhere else it would be a
// setting nothing reads.
func TestASelectionWhereNothingReadsItIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, typ, driver, op string }{
		{"GraphQL server", "graphql", "server", "Subscription.priceChanged"},
		{"a query on the client", "graphql", "client", "Query.products"},
		{"another connector", "rest", "", "GET /products"},
	} {
		errs := ValidateSubscriptionSelections(selectionConfig(tc.typ, tc.driver, tc.op, "{ sku }"))
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "only applies") {
			t.Errorf("%s: want the selection refused as unread, got %v", tc.name, errs)
		}
	}
}

func TestNoSelectionNeedsNoCheck(t *testing.T) {
	if errs := ValidateSubscriptionSelections(selectionConfig("graphql", "client", "Subscription.priceChanged", "")); len(errs) != 0 {
		t.Fatalf("no selection was refused: %v", errs)
	}
}

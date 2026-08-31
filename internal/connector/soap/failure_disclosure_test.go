package soap

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWhatAProductionFaultSays(t *testing.T) {
	// The REST connector was the only server that asked what environment it
	// was running in. This one put the raw error text in the faultstring
	// wherever it ran, so a driver message — table names, hosts, fragments of
	// a query — reached whoever called the endpoint.
	s := serverFor(t, "1.1")
	s.environment = "production"
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("query failed: no such table: internal_billing_table")
	})

	body := call(t, s, envelopeFor("CreateOrder", `<CustomerId>customer-1</CustomerId>`)).Body.String()

	if strings.Contains(body, "internal_billing_table") {
		t.Errorf("the name of an internal table was sent to the caller:\n%s", body)
	}
	// It is still a fault, and still the server's: a SOAP client gets nothing
	// out of a response it cannot parse, and Server is what tells it the
	// failure is worth retrying.
	if !strings.Contains(body, "Fault") || !strings.Contains(body, "Server") {
		t.Errorf("a failure stopped coming back as a server fault:\n%s", body)
	}
}

func TestWhatADevelopmentFaultSays(t *testing.T) {
	// Withholding it from a developer helps nobody: the whole point of running
	// outside production is being told what actually broke.
	s := serverFor(t, "1.1")
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("query failed: no such table: internal_billing_table")
	})

	body := call(t, s, envelopeFor("CreateOrder", `<CustomerId>customer-1</CustomerId>`)).Body.String()

	if !strings.Contains(body, "internal_billing_table") {
		t.Errorf("a developer was not told what actually failed:\n%s", body)
	}
}

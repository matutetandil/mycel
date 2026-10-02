package main

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// The validate summary names where a flow writes. A destination without a
// target — a transaction, or a write given as a query — used to print an arrow
// pointing at nothing.
func TestTheValidateSummaryNamesEveryKindOfDestination(t *testing.T) {
	tests := []struct {
		name string
		f    *flow.Config
		want string
	}{
		{"a target", &flow.Config{To: &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "users"}}}, "users"},
		{"a transaction", &flow.Config{To: &flow.ToConfig{Connector: "db", Transaction: &flow.TransactionConfig{}}}, "db (transaction)"},
		{"a query and no target", &flow.Config{To: &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"query": "INSERT ..."}}}, "db"},
		{"several destinations", &flow.Config{MultiTo: []*flow.ToConfig{{Connector: "a"}, {Connector: "b"}}}, "2 destinations"},
		{"no destination", &flow.Config{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := flowDestinationSummary(tc.f); got != tc.want {
				t.Errorf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

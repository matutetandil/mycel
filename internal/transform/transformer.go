package transform

import "strings"

// The native expression engine that used to live here is gone.
//
// It was a complete second implementation — a hand-written parser and a
// registry of twenty-three functions — superseded by CEL and left in place.
// Nothing constructed it: every caller in the runtime, the aspects and the CLI
// used NewCELTransformer, no configuration selected it, and the ten functions
// it had that CEL does not (to_int, concat, parse_date and the rest) appear in
// no documentation, so nothing pointed at it either. Its own tests were the
// only thing keeping it compiled.
//
// What remains is the configuration it shared with the engine that runs.

// Rule represents a single transformation rule.
type Rule struct {
	// Target is the output field path (e.g., "email" or "user.email").
	Target string

	// Expression is the transformation expression (e.g., "lower(input.email)").
	Expression string
}

// Config holds transform configuration from HCL.
type Config struct {
	// Name is the transform identifier (for named transforms).
	Name string

	// Mappings are the transformation rules.
	// Keys are output field paths, values are expressions.
	Mappings map[string]string

	// Order lists the Mappings keys in the order they were declared in the
	// source file, so that an expression referencing a field computed above it
	// through `output` resolves the same way on every message. See
	// RulesFromMappings.
	Order []string

	// Enrichments are data lookups from external sources.
	// These are executed before mappings and results are available as enriched.*
	Enrichments []*EnrichConfig
}

// EnrichConfig holds configuration for enriching data from external sources.
// This is a copy of flow.EnrichConfig to avoid circular imports.
type EnrichConfig struct {
	// Name is the identifier for this enrichment (used as enriched.<name>).
	Name string

	// Connector is the connector to use for the lookup.
	Connector string

	// Operation is the operation to perform on the connector.
	Operation string

	// Params are the parameters to pass to the operation.
	// Keys are parameter names, values are CEL expressions.
	Params map[string]string
}

// setNestedValue sets a value at a nested path in a map.
func setNestedValue(data map[string]interface{}, path string, value interface{}) error {
	parts := strings.Split(path, ".")

	// Navigate to the parent, creating nested maps as needed
	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			// Create nested map
			next := make(map[string]interface{})
			current[part] = next
			current = next
		}
	}

	// Set the final value
	current[parts[len(parts)-1]] = value
	return nil
}

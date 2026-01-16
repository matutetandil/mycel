// Package optimizer provides SQL query optimization based on GraphQL requested fields.
// It rewrites SELECT * queries to only fetch the columns that are actually requested.
package optimizer

import (
	"regexp"
	"strings"

	"github.com/matutetandil/mycel/internal/graphql/analyzer"
)

// SQLOptimizer optimizes SQL queries based on requested fields.
type SQLOptimizer struct {
	requestedFields *analyzer.RequestedFields
	columnMapping   map[string]string // GraphQL field -> DB column mapping
}

// NewSQLOptimizer creates a new SQL optimizer.
func NewSQLOptimizer(fields *analyzer.RequestedFields) *SQLOptimizer {
	return &SQLOptimizer{
		requestedFields: fields,
		columnMapping:   make(map[string]string),
	}
}

// SetColumnMapping sets custom GraphQL field to DB column mappings.
// By default, camelCase fields are converted to snake_case columns.
func (o *SQLOptimizer) SetColumnMapping(mapping map[string]string) {
	o.columnMapping = mapping
}

// OptimizeQuery optimizes a SQL query to only select requested columns.
// Returns the optimized query and a boolean indicating if optimization was applied.
func (o *SQLOptimizer) OptimizeQuery(query string) (string, bool) {
	if o.requestedFields == nil || o.requestedFields.IsEmpty() {
		return query, false
	}

	// Normalize whitespace
	query = normalizeWhitespace(query)

	// Check if it's a SELECT * query
	if !isSelectStar(query) {
		return query, false
	}

	// Get the columns to select
	columns := o.getColumnsToSelect()
	if len(columns) == 0 {
		return query, false
	}

	// Rewrite the query
	optimized := rewriteSelectStar(query, columns)
	return optimized, optimized != query
}

// OptimizeQueryWithFields optimizes a query using a list of field names.
// Useful when fields come from input.__requested_fields instead of analyzer.
func OptimizeQueryWithFields(query string, fields []string) (string, bool) {
	if len(fields) == 0 {
		return query, false
	}

	query = normalizeWhitespace(query)

	if !isSelectStar(query) {
		return query, false
	}

	// Convert field names to column names
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		// Only use top-level fields (no dots)
		if !strings.Contains(field, ".") {
			columns = append(columns, CamelToSnake(field))
		}
	}

	if len(columns) == 0 {
		return query, false
	}

	optimized := rewriteSelectStar(query, columns)
	return optimized, optimized != query
}

// getColumnsToSelect returns the list of DB columns to select.
func (o *SQLOptimizer) getColumnsToSelect() []string {
	fields := o.requestedFields.List()
	columns := make([]string, 0, len(fields))

	for _, field := range fields {
		// Skip nested fields (e.g., "orders.total")
		if strings.Contains(field, ".") {
			continue
		}

		// Check for custom mapping
		if col, ok := o.columnMapping[field]; ok {
			columns = append(columns, col)
		} else {
			// Convert camelCase to snake_case
			columns = append(columns, CamelToSnake(field))
		}
	}

	return columns
}

// isSelectStar checks if a query is a SELECT * query.
func isSelectStar(query string) bool {
	// Pattern: SELECT * FROM ... or SELECT DISTINCT * FROM ...
	pattern := regexp.MustCompile(`(?i)^\s*SELECT\s+(DISTINCT\s+)?\*\s+FROM\s+`)
	return pattern.MatchString(query)
}

// rewriteSelectStar replaces SELECT * with SELECT columns.
func rewriteSelectStar(query string, columns []string) string {
	// Pattern to match SELECT * or SELECT DISTINCT *
	pattern := regexp.MustCompile(`(?i)(SELECT\s+)(DISTINCT\s+)?(\*)\s+(FROM)`)

	return pattern.ReplaceAllStringFunc(query, func(match string) string {
		// Preserve DISTINCT if present
		distinctMatch := regexp.MustCompile(`(?i)DISTINCT\s+`)
		distinct := ""
		if distinctMatch.MatchString(match) {
			distinct = "DISTINCT "
		}

		return "SELECT " + distinct + strings.Join(columns, ", ") + " FROM"
	})
}

// normalizeWhitespace normalizes whitespace in a query.
func normalizeWhitespace(query string) string {
	// Replace multiple spaces with single space
	space := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(space.ReplaceAllString(query, " "))
}

// CamelToSnake converts a camelCase string to snake_case.
func CamelToSnake(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, c+32) // to lowercase
		} else {
			result = append(result, c)
		}
	}
	return string(result)
}

// FieldsFromInput extracts the __requested_fields from an input map.
func FieldsFromInput(input map[string]interface{}) []string {
	if input == nil {
		return nil
	}

	if fields, ok := input["__requested_fields"].([]interface{}); ok {
		result := make([]string, len(fields))
		for i, f := range fields {
			if s, ok := f.(string); ok {
				result[i] = s
			}
		}
		return result
	}

	if fields, ok := input["__requested_fields"].([]string); ok {
		return fields
	}

	return nil
}

// TopFieldsFromInput extracts the __requested_top_fields from an input map.
func TopFieldsFromInput(input map[string]interface{}) []string {
	if input == nil {
		return nil
	}

	if fields, ok := input["__requested_top_fields"].([]interface{}); ok {
		result := make([]string, len(fields))
		for i, f := range fields {
			if s, ok := f.(string); ok {
				result[i] = s
			}
		}
		return result
	}

	if fields, ok := input["__requested_top_fields"].([]string); ok {
		return fields
	}

	return nil
}

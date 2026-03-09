package rules

import (
	"fmt"
	"regexp"
)

// validSQLIdentifier matches safe SQL identifiers: letters, digits, underscores.
var validSQLIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// SQLIdentifierRule validates that identifier-position values (table/column names)
// contain only safe characters. This prevents SQL injection in dynamic identifier usage.
type SQLIdentifierRule struct {
	// IdentifierFields are the field names that should be treated as SQL identifiers.
	// If empty, no fields are validated (only applied when connector provides field names).
	IdentifierFields []string
}

func (r *SQLIdentifierRule) Name() string { return "sql_identifier" }

func (r *SQLIdentifierRule) Sanitize(value interface{}) (interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return value, nil
	}

	for _, field := range r.IdentifierFields {
		if v, exists := m[field]; exists {
			if s, ok := v.(string); ok {
				if !ValidateSQLIdentifier(s) {
					return nil, fmt.Errorf("field %q contains invalid SQL identifier %q", field, s)
				}
			}
		}
	}

	return value, nil
}

// ValidateSQLIdentifier checks if a string is a safe SQL identifier.
func ValidateSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	return validSQLIdentifier.MatchString(s)
}

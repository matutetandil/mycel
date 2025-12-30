package graphql

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// JSONScalar is a scalar type that represents arbitrary JSON data.
var JSONScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "JSON",
	Description: "The JSON scalar type represents arbitrary JSON data.",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		return value
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		return parseLiteralValue(valueAST)
	},
})

// DateTimeScalar is a scalar type for ISO 8601 datetime strings.
var DateTimeScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "DateTime",
	Description: "The DateTime scalar represents a date-time as specified by ISO 8601.",
	Serialize: func(value interface{}) interface{} {
		switch v := value.(type) {
		case time.Time:
			return v.Format(time.RFC3339)
		case *time.Time:
			if v == nil {
				return nil
			}
			return v.Format(time.RFC3339)
		case string:
			return v
		default:
			return nil
		}
	},
	ParseValue: func(value interface{}) interface{} {
		switch v := value.(type) {
		case string:
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				// Try other common formats
				formats := []string{
					"2006-01-02T15:04:05Z07:00",
					"2006-01-02T15:04:05",
					"2006-01-02 15:04:05",
					"2006-01-02",
				}
				for _, format := range formats {
					if t, err = time.Parse(format, v); err == nil {
						return t
					}
				}
				return nil
			}
			return t
		case time.Time:
			return v
		default:
			return nil
		}
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch v := valueAST.(type) {
		case *ast.StringValue:
			t, err := time.Parse(time.RFC3339, v.Value)
			if err != nil {
				return nil
			}
			return t
		default:
			return nil
		}
	},
})

// DateScalar is a scalar type for date-only strings (YYYY-MM-DD).
var DateScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "Date",
	Description: "The Date scalar represents a date in YYYY-MM-DD format.",
	Serialize: func(value interface{}) interface{} {
		switch v := value.(type) {
		case time.Time:
			return v.Format("2006-01-02")
		case *time.Time:
			if v == nil {
				return nil
			}
			return v.Format("2006-01-02")
		case string:
			return v
		default:
			return nil
		}
	},
	ParseValue: func(value interface{}) interface{} {
		switch v := value.(type) {
		case string:
			t, err := time.Parse("2006-01-02", v)
			if err != nil {
				return nil
			}
			return t
		case time.Time:
			return v
		default:
			return nil
		}
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch v := valueAST.(type) {
		case *ast.StringValue:
			t, err := time.Parse("2006-01-02", v.Value)
			if err != nil {
				return nil
			}
			return t
		default:
			return nil
		}
	},
})

// TimeScalar is a scalar type for time-only strings (HH:MM:SS).
var TimeScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "Time",
	Description: "The Time scalar represents a time in HH:MM:SS format.",
	Serialize: func(value interface{}) interface{} {
		switch v := value.(type) {
		case time.Time:
			return v.Format("15:04:05")
		case *time.Time:
			if v == nil {
				return nil
			}
			return v.Format("15:04:05")
		case string:
			return v
		default:
			return nil
		}
	},
	ParseValue: func(value interface{}) interface{} {
		switch v := value.(type) {
		case string:
			t, err := time.Parse("15:04:05", v)
			if err != nil {
				// Try without seconds
				t, err = time.Parse("15:04", v)
				if err != nil {
					return nil
				}
			}
			return t
		case time.Time:
			return v
		default:
			return nil
		}
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch v := valueAST.(type) {
		case *ast.StringValue:
			t, err := time.Parse("15:04:05", v.Value)
			if err != nil {
				return nil
			}
			return t
		default:
			return nil
		}
	},
})

// PositiveIntScalar is a scalar type for positive integers.
var PositiveIntScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "PositiveInt",
	Description: "A positive integer (greater than 0).",
	Serialize: func(value interface{}) interface{} {
		switch v := value.(type) {
		case int:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return int(v)
			}
		case float64:
			if v > 0 {
				return int(v)
			}
		}
		return nil
	},
	ParseValue: func(value interface{}) interface{} {
		switch v := value.(type) {
		case int:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return int(v)
			}
		case float64:
			if v > 0 {
				return int(v)
			}
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch v := valueAST.(type) {
		case *ast.IntValue:
			// Parse and validate
			var i int
			if _, err := fmt.Sscanf(v.Value, "%d", &i); err == nil && i > 0 {
				return i
			}
		}
		return nil
	},
})

// NonNegativeIntScalar is a scalar type for non-negative integers.
var NonNegativeIntScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "NonNegativeInt",
	Description: "A non-negative integer (0 or greater).",
	Serialize: func(value interface{}) interface{} {
		switch v := value.(type) {
		case int:
			if v >= 0 {
				return v
			}
		case int64:
			if v >= 0 {
				return int(v)
			}
		case float64:
			if v >= 0 {
				return int(v)
			}
		}
		return nil
	},
	ParseValue: func(value interface{}) interface{} {
		switch v := value.(type) {
		case int:
			if v >= 0 {
				return v
			}
		case int64:
			if v >= 0 {
				return int(v)
			}
		case float64:
			if v >= 0 {
				return int(v)
			}
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch v := valueAST.(type) {
		case *ast.IntValue:
			var i int
			if _, err := fmt.Sscanf(v.Value, "%d", &i); err == nil && i >= 0 {
				return i
			}
		}
		return nil
	},
})

// EmailScalar is a scalar type for email addresses.
var EmailScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "Email",
	Description: "A valid email address.",
	Serialize: func(value interface{}) interface{} {
		if s, ok := value.(string); ok {
			return s
		}
		return nil
	},
	ParseValue: func(value interface{}) interface{} {
		if s, ok := value.(string); ok {
			// Basic email validation
			if isValidEmail(s) {
				return s
			}
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		if v, ok := valueAST.(*ast.StringValue); ok {
			if isValidEmail(v.Value) {
				return v.Value
			}
		}
		return nil
	},
})

// URLScalar is a scalar type for URLs.
var URLScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "URL",
	Description: "A valid URL.",
	Serialize: func(value interface{}) interface{} {
		if s, ok := value.(string); ok {
			return s
		}
		return nil
	},
	ParseValue: func(value interface{}) interface{} {
		if s, ok := value.(string); ok {
			if isValidURL(s) {
				return s
			}
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		if v, ok := valueAST.(*ast.StringValue); ok {
			if isValidURL(v.Value) {
				return v.Value
			}
		}
		return nil
	},
})

// UUIDScalar is a scalar type for UUIDs.
var UUIDScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "UUID",
	Description: "A valid UUID (v4).",
	Serialize: func(value interface{}) interface{} {
		if s, ok := value.(string); ok {
			return s
		}
		return nil
	},
	ParseValue: func(value interface{}) interface{} {
		if s, ok := value.(string); ok {
			if isValidUUID(s) {
				return s
			}
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		if v, ok := valueAST.(*ast.StringValue); ok {
			if isValidUUID(v.Value) {
				return v.Value
			}
		}
		return nil
	},
})

// parseLiteralValue parses an AST value to a Go value.
func parseLiteralValue(valueAST ast.Value) interface{} {
	if valueAST == nil {
		return nil
	}

	switch v := valueAST.(type) {
	case *ast.StringValue:
		return v.Value
	case *ast.IntValue:
		var i int64
		if _, err := fmt.Sscanf(v.Value, "%d", &i); err == nil {
			return i
		}
		return v.Value
	case *ast.FloatValue:
		var f float64
		if _, err := fmt.Sscanf(v.Value, "%f", &f); err == nil {
			return f
		}
		return v.Value
	case *ast.BooleanValue:
		return v.Value
	case *ast.EnumValue:
		return v.Value
	case *ast.ListValue:
		result := make([]interface{}, len(v.Values))
		for i, item := range v.Values {
			result[i] = parseLiteralValue(item)
		}
		return result
	case *ast.ObjectValue:
		result := make(map[string]interface{})
		for _, field := range v.Fields {
			result[field.Name.Value] = parseLiteralValue(field.Value)
		}
		return result
	default:
		return nil
	}
}

// Helper validation functions

func isValidEmail(email string) bool {
	// Simple email validation
	if len(email) < 3 || len(email) > 254 {
		return false
	}
	atIdx := -1
	for i, c := range email {
		if c == '@' {
			if atIdx != -1 {
				return false // Multiple @
			}
			atIdx = i
		}
	}
	if atIdx < 1 || atIdx > len(email)-3 {
		return false
	}
	// Check for dot after @
	domain := email[atIdx+1:]
	dotIdx := -1
	for i, c := range domain {
		if c == '.' {
			dotIdx = i
		}
	}
	return dotIdx > 0 && dotIdx < len(domain)-1
}

func isValidURL(url string) bool {
	// Simple URL validation
	if len(url) < 10 {
		return false
	}
	return len(url) >= 7 && (url[:7] == "http://" || (len(url) >= 8 && url[:8] == "https://"))
}

func isValidUUID(uuid string) bool {
	// Simple UUID validation (8-4-4-4-12 format)
	if len(uuid) != 36 {
		return false
	}
	for i, c := range uuid {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// ScalarMap maps scalar type names to their implementations.
var ScalarMap = map[string]*graphql.Scalar{
	"JSON":           JSONScalar,
	"DateTime":       DateTimeScalar,
	"Date":           DateScalar,
	"Time":           TimeScalar,
	"PositiveInt":    PositiveIntScalar,
	"NonNegativeInt": NonNegativeIntScalar,
	"Email":          EmailScalar,
	"URL":            URLScalar,
	"UUID":           UUIDScalar,
}

// GetScalar returns a scalar type by name.
func GetScalar(name string) *graphql.Scalar {
	return ScalarMap[name]
}

// SerializeToJSON serializes a value to a JSON string.
func SerializeToJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ParseFromJSON parses a JSON string to a Go value.
func ParseFromJSON(data string) (interface{}, error) {
	var result interface{}
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, err
	}
	return result, nil
}

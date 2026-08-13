package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/matutetandil/mycel/v2/internal/validate"
)

// SDLGenerator generates GraphQL SDL from various sources.
type SDLGenerator struct {
	// parsedSchema contains types from SDL parsing.
	parsedSchema *ParsedSchema

	// hclConverter contains types from HCL conversion.
	hclConverter *HCLConverter

	// sdlConverter contains types from SDL conversion.
	sdlConverter *SDLConverter

	// queryFields are the Query type fields.
	queryFields graphql.Fields

	// mutationFields are the Mutation type fields.
	mutationFields graphql.Fields

	// subscriptionFields are the Subscription type fields.
	subscriptionFields graphql.Fields

	// federation support configuration.
	federation *FederationSupport

	// customScalars lists custom scalar names to include.
	customScalars []string

	// includeDescriptions includes field/type descriptions in SDL.
	includeDescriptions bool
}

// NewSDLGenerator creates a new SDL generator.
func NewSDLGenerator() *SDLGenerator {
	return &SDLGenerator{
		queryFields:         make(graphql.Fields),
		mutationFields:      make(graphql.Fields),
		subscriptionFields:  make(graphql.Fields),
		customScalars:       []string{},
		includeDescriptions: true,
	}
}

// GenerateFromTypeSchemas generates SDL directly from TypeSchemas.
func (g *SDLGenerator) GenerateFromTypeSchemas(schemas map[string]*validate.TypeSchema) string {
	var sb strings.Builder

	// Default scalars
	sb.WriteString("scalar JSON\n")
	sb.WriteString("scalar DateTime\n")
	sb.WriteString("scalar Date\n")
	sb.WriteString("scalar Time\n\n")

	// Sort type names for deterministic output
	typeNames := make([]string, 0, len(schemas))
	for name := range schemas {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)

	// Generate type and input for each schema
	for _, name := range typeNames {
		schema := schemas[name]

		// Generate object type
		sb.WriteString(fmt.Sprintf("type %s {\n", name))
		for _, field := range schema.Fields {
			gqlType := g.mapFieldTypeToSDL(&field)
			if field.Required {
				gqlType += "!"
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", field.Name, gqlType))
		}
		sb.WriteString("}\n\n")

		// Generate input type
		sb.WriteString(fmt.Sprintf("input %sInput {\n", name))
		for _, field := range schema.Fields {
			gqlType := g.mapFieldTypeToSDL(&field)
			if field.Required {
				gqlType += "!"
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", field.Name, gqlType))
		}
		sb.WriteString("}\n\n")
	}

	return sb.String()
}

// mapFieldTypeToSDL maps a FieldSchema type to SDL.
func (g *SDLGenerator) mapFieldTypeToSDL(field *validate.FieldSchema) string {
	// Check for enum constraint
	for _, constraint := range field.Constraints {
		if _, ok := constraint.(*validate.EnumConstraint); ok {
			return toPascalCase(field.Name) + "Enum"
		}
	}

	// Check for format constraint
	for _, constraint := range field.Constraints {
		if fc, ok := constraint.(*validate.FormatConstraint); ok {
			switch fc.Format {
			case "email":
				return "String" // Email is typically just a String with validation
			case "url":
				return "String"
			case "uuid":
				return "ID"
			case "date":
				return "Date"
			case "datetime":
				return "DateTime"
			}
		}
	}

	// Map base types
	switch field.Type {
	case "string":
		return "String"
	case "number":
		// Check if it's an integer
		for _, constraint := range field.Constraints {
			switch constraint.(type) {
			case *validate.MinConstraint, *validate.MaxConstraint:
				return "Int" // Assume integer if has min/max
			}
		}
		return "Float"
	case "boolean":
		return "Boolean"
	case "id":
		return "ID"
	case "array":
		return "[JSON]"
	case "object":
		return "JSON"
	default:
		return field.Type // Might be a reference to another type
	}
}

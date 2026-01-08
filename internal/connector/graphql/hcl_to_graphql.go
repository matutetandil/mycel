package graphql

import (
	"fmt"
	"strings"
	"sync"

	"github.com/graphql-go/graphql"
	"github.com/matutetandil/mycel/internal/validate"
)

// HCLConverter converts HCL TypeSchemas to GraphQL types.
type HCLConverter struct {
	mu sync.RWMutex

	// types holds converted object types.
	types map[string]*graphql.Object

	// inputs holds converted input object types.
	inputs map[string]*graphql.InputObject

	// enums holds generated enum types.
	enums map[string]*graphql.Enum

	// typeSchemas holds the source type schemas.
	typeSchemas map[string]*validate.TypeSchema
}

// NewHCLConverter creates a new HCL converter.
func NewHCLConverter() *HCLConverter {
	return &HCLConverter{
		types:       make(map[string]*graphql.Object),
		inputs:      make(map[string]*graphql.InputObject),
		enums:       make(map[string]*graphql.Enum),
		typeSchemas: make(map[string]*validate.TypeSchema),
	}
}

// LoadTypeSchemas loads all type schemas from HCL types configuration.
func (c *HCLConverter) LoadTypeSchemas(schemas map[string]*validate.TypeSchema) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, schema := range schemas {
		c.typeSchemas[name] = schema
	}
}

// Convert converts all loaded type schemas to GraphQL types.
func (c *HCLConverter) Convert() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// First pass: create placeholders for all types
	for name := range c.typeSchemas {
		c.types[name] = graphql.NewObject(graphql.ObjectConfig{
			Name:   name,
			Fields: graphql.Fields{},
		})
	}

	// Second pass: fill in fields
	for name, schema := range c.typeSchemas {
		if err := c.convertTypeSchema(name, schema); err != nil {
			return fmt.Errorf("failed to convert type %s: %w", name, err)
		}
	}

	return nil
}

// ConvertTypeSchema converts a single TypeSchema to a GraphQL object type.
func (c *HCLConverter) ConvertTypeSchema(schema *validate.TypeSchema) (*graphql.Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already converted
	if t, exists := c.types[schema.Name]; exists {
		return t, nil
	}

	// Store the schema
	c.typeSchemas[schema.Name] = schema

	// Convert
	if err := c.convertTypeSchema(schema.Name, schema); err != nil {
		return nil, err
	}

	return c.types[schema.Name], nil
}

// convertTypeSchema converts a TypeSchema and fills in the graphql.Object fields.
func (c *HCLConverter) convertTypeSchema(name string, schema *validate.TypeSchema) error {
	fields := graphql.Fields{}

	for _, field := range schema.Fields {
		gqlField, err := c.convertField(&field)
		if err != nil {
			return fmt.Errorf("failed to convert field %s: %w", field.Name, err)
		}
		fields[field.Name] = gqlField
	}

	// Use schema description if provided, otherwise generate one
	description := schema.Description
	if description == "" {
		description = fmt.Sprintf("Type %s generated from HCL", name)
	}

	// Create the object type with fields
	c.types[name] = graphql.NewObject(graphql.ObjectConfig{
		Name:        name,
		Description: description,
		Fields:      fields,
	})

	// Also create an input type for mutations
	c.inputs[name+"Input"] = c.createInputType(name, schema)

	return nil
}

// convertField converts a FieldSchema to a graphql.Field.
func (c *HCLConverter) convertField(field *validate.FieldSchema) (*graphql.Field, error) {
	gqlType, err := c.mapHCLType(field)
	if err != nil {
		return nil, err
	}

	// Apply non-null if required (but not for external fields)
	if field.Required && !field.External {
		gqlType = graphql.NewNonNull(gqlType)
	}

	// Use field description if provided
	description := field.Description
	if description == "" {
		description = fmt.Sprintf("Field %s", field.Name)
	}

	return &graphql.Field{
		Type:        gqlType,
		Description: description,
	}, nil
}

// mapHCLType maps an HCL type to a GraphQL type.
func (c *HCLConverter) mapHCLType(field *validate.FieldSchema) (graphql.Output, error) {
	// Check for enum constraint first
	for _, constraint := range field.Constraints {
		if ec, ok := constraint.(*validate.EnumConstraint); ok {
			return c.createEnumFromConstraint(field.Name, ec.Values), nil
		}
	}

	// Check for format constraint
	for _, constraint := range field.Constraints {
		if fc, ok := constraint.(*validate.FormatConstraint); ok {
			switch fc.Format {
			case "email":
				return EmailScalar, nil
			case "url":
				return URLScalar, nil
			case "uuid":
				return UUIDScalar, nil
			case "date":
				return DateScalar, nil
			case "datetime":
				return DateTimeScalar, nil
			}
		}
	}

	// Map base types
	switch field.Type {
	case "string":
		return graphql.String, nil
	case "number":
		// Check if it's an integer (has min/max constraints that are integers)
		isInt := c.isIntegerField(field)
		if isInt {
			return graphql.Int, nil
		}
		return graphql.Float, nil
	case "boolean":
		return graphql.Boolean, nil
	case "id":
		return graphql.ID, nil
	case "array":
		// For arrays, we need item type info
		// Default to JSON array if no item type specified
		return graphql.NewList(JSONScalar), nil
	case "object":
		// For nested objects, default to JSON
		return JSONScalar, nil
	default:
		// Check if it references another type
		if t, exists := c.types[field.Type]; exists {
			return t, nil
		}
		// Fallback to JSON for unknown types
		return JSONScalar, nil
	}
}

// isIntegerField checks if the field's constraints suggest it's an integer.
func (c *HCLConverter) isIntegerField(field *validate.FieldSchema) bool {
	for _, constraint := range field.Constraints {
		switch con := constraint.(type) {
		case *validate.MinConstraint:
			if con.Min == float64(int64(con.Min)) {
				return true
			}
		case *validate.MaxConstraint:
			if con.Max == float64(int64(con.Max)) {
				return true
			}
		}
	}
	return false
}

// createEnumFromConstraint creates a GraphQL enum from an EnumConstraint.
func (c *HCLConverter) createEnumFromConstraint(fieldName string, values []string) *graphql.Enum {
	enumName := toPascalCase(fieldName) + "Enum"

	// Check if already created
	if e, exists := c.enums[enumName]; exists {
		return e
	}

	enumValues := graphql.EnumValueConfigMap{}
	for _, value := range values {
		enumValues[toEnumValue(value)] = &graphql.EnumValueConfig{
			Value:       value,
			Description: fmt.Sprintf("Value: %s", value),
		}
	}

	enum := graphql.NewEnum(graphql.EnumConfig{
		Name:        enumName,
		Description: fmt.Sprintf("Enum for %s field", fieldName),
		Values:      enumValues,
	})

	c.enums[enumName] = enum
	return enum
}

// createInputType creates a GraphQL input type from a TypeSchema.
func (c *HCLConverter) createInputType(name string, schema *validate.TypeSchema) *graphql.InputObject {
	fields := graphql.InputObjectConfigFieldMap{}

	for _, field := range schema.Fields {
		inputType := c.mapHCLTypeToInput(&field)

		if field.Required {
			inputType = graphql.NewNonNull(inputType)
		}

		fields[field.Name] = &graphql.InputObjectFieldConfig{
			Type:        inputType,
			Description: fmt.Sprintf("Input field %s", field.Name),
		}
	}

	return graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        name + "Input",
		Description: fmt.Sprintf("Input type for %s", name),
		Fields:      fields,
	})
}

// mapHCLTypeToInput maps an HCL type to a GraphQL input type.
func (c *HCLConverter) mapHCLTypeToInput(field *validate.FieldSchema) graphql.Input {
	// Check for enum constraint
	for _, constraint := range field.Constraints {
		if ec, ok := constraint.(*validate.EnumConstraint); ok {
			return c.createEnumFromConstraint(field.Name, ec.Values)
		}
	}

	// Check for format constraint
	for _, constraint := range field.Constraints {
		if fc, ok := constraint.(*validate.FormatConstraint); ok {
			switch fc.Format {
			case "email":
				return EmailScalar
			case "url":
				return URLScalar
			case "uuid":
				return UUIDScalar
			case "date":
				return DateScalar
			case "datetime":
				return DateTimeScalar
			}
		}
	}

	switch field.Type {
	case "string":
		return graphql.String
	case "number":
		if c.isIntegerField(field) {
			return graphql.Int
		}
		return graphql.Float
	case "boolean":
		return graphql.Boolean
	case "id":
		return graphql.ID
	case "array":
		return graphql.NewList(JSONScalar)
	case "object":
		return JSONScalar
	default:
		// Check if it references another input type
		if input, exists := c.inputs[field.Type+"Input"]; exists {
			return input
		}
		return JSONScalar
	}
}

// GetType returns a converted type by name.
func (c *HCLConverter) GetType(name string) *graphql.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.types[name]
}

// GetInput returns a converted input type by name.
func (c *HCLConverter) GetInput(name string) *graphql.InputObject {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inputs[name]
}

// GetEnum returns a generated enum by name.
func (c *HCLConverter) GetEnum(name string) *graphql.Enum {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enums[name]
}

// AllTypes returns all converted object types.
func (c *HCLConverter) AllTypes() map[string]*graphql.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*graphql.Object, len(c.types))
	for k, v := range c.types {
		result[k] = v
	}
	return result
}

// AllInputs returns all converted input types.
func (c *HCLConverter) AllInputs() map[string]*graphql.InputObject {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*graphql.InputObject, len(c.inputs))
	for k, v := range c.inputs {
		result[k] = v
	}
	return result
}

// AllEnums returns all generated enum types.
func (c *HCLConverter) AllEnums() map[string]*graphql.Enum {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*graphql.Enum, len(c.enums))
	for k, v := range c.enums {
		result[k] = v
	}
	return result
}

// GenerateSDL generates SDL from converted types.
func (c *HCLConverter) GenerateSDL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder

	// Generate enum definitions
	for name, enum := range c.enums {
		sb.WriteString(fmt.Sprintf("enum %s {\n", name))
		for _, val := range enum.Values() {
			sb.WriteString(fmt.Sprintf("  %s\n", val.Name))
		}
		sb.WriteString("}\n\n")
	}

	// Generate type definitions with federation directives
	for name, schema := range c.typeSchemas {
		sb.WriteString(fmt.Sprintf("type %s", name))

		// Add type-level federation directives
		directives := c.typeDirectivesToSDL(schema)
		if directives != "" {
			sb.WriteString(" " + directives)
		}

		sb.WriteString(" {\n")
		for _, field := range schema.Fields {
			gqlType := c.fieldTypeToSDL(&field)
			if field.Required && !field.External {
				gqlType += "!"
			}
			fieldDirectives := c.fieldDirectivesToSDL(&field)
			sb.WriteString(fmt.Sprintf("  %s: %s%s\n", field.Name, gqlType, fieldDirectives))
		}
		sb.WriteString("}\n\n")
	}

	// Generate input type definitions (no federation directives on inputs)
	for name, schema := range c.typeSchemas {
		sb.WriteString(fmt.Sprintf("input %sInput {\n", name))
		for _, field := range schema.Fields {
			// Skip external fields in input types
			if field.External {
				continue
			}
			gqlType := c.fieldTypeToSDL(&field)
			if field.Required {
				gqlType += "!"
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", field.Name, gqlType))
		}
		sb.WriteString("}\n\n")
	}

	return sb.String()
}

// typeDirectivesToSDL generates federation directives for a type.
func (c *HCLConverter) typeDirectivesToSDL(schema *validate.TypeSchema) string {
	var directives []string

	// @key directive(s)
	for _, key := range schema.Keys {
		directives = append(directives, fmt.Sprintf("@key(fields: \"%s\")", key))
	}

	// @shareable directive
	if schema.Shareable {
		directives = append(directives, "@shareable")
	}

	// @inaccessible directive
	if schema.Inaccessible {
		directives = append(directives, "@inaccessible")
	}

	// implements interfaces
	if len(schema.InterfaceNames) > 0 {
		directives = append(directives, fmt.Sprintf("implements %s", strings.Join(schema.InterfaceNames, " & ")))
	}

	return strings.Join(directives, " ")
}

// fieldDirectivesToSDL generates federation directives for a field.
func (c *HCLConverter) fieldDirectivesToSDL(field *validate.FieldSchema) string {
	var directives []string

	// @external directive
	if field.External {
		directives = append(directives, "@external")
	}

	// @provides directive
	if field.Provides != "" {
		directives = append(directives, fmt.Sprintf("@provides(fields: \"%s\")", field.Provides))
	}

	// @requires directive
	if field.Requires != "" {
		directives = append(directives, fmt.Sprintf("@requires(fields: \"%s\")", field.Requires))
	}

	// @shareable directive
	if field.Shareable {
		directives = append(directives, "@shareable")
	}

	// @inaccessible directive
	if field.Inaccessible {
		directives = append(directives, "@inaccessible")
	}

	// @override directive
	if field.Override != "" {
		directives = append(directives, fmt.Sprintf("@override(from: \"%s\")", field.Override))
	}

	if len(directives) == 0 {
		return ""
	}
	return " " + strings.Join(directives, " ")
}

// fieldTypeToSDL converts a field type to SDL string representation.
func (c *HCLConverter) fieldTypeToSDL(field *validate.FieldSchema) string {
	// Check for enum
	for _, constraint := range field.Constraints {
		if _, ok := constraint.(*validate.EnumConstraint); ok {
			return toPascalCase(field.Name) + "Enum"
		}
	}

	// Check for format
	for _, constraint := range field.Constraints {
		if fc, ok := constraint.(*validate.FormatConstraint); ok {
			switch fc.Format {
			case "email":
				return "Email"
			case "url":
				return "URL"
			case "uuid":
				return "UUID"
			case "date":
				return "Date"
			case "datetime":
				return "DateTime"
			}
		}
	}

	switch field.Type {
	case "string":
		return "String"
	case "number":
		if c.isIntegerField(field) {
			return "Int"
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
		// Check if it's a known type
		if _, exists := c.types[field.Type]; exists {
			return field.Type
		}
		return "JSON"
	}
}

// Helper functions

// toPascalCase converts a string to PascalCase.
func toPascalCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	var result strings.Builder
	for _, word := range words {
		if len(word) > 0 {
			result.WriteString(strings.ToUpper(string(word[0])))
			if len(word) > 1 {
				result.WriteString(strings.ToLower(word[1:]))
			}
		}
	}

	if result.Len() == 0 {
		return strings.Title(s)
	}

	return result.String()
}

// toEnumValue converts a string to a valid GraphQL enum value.
func toEnumValue(s string) string {
	// GraphQL enum values must be uppercase and can contain underscores
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")

	// Remove any invalid characters
	var result strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

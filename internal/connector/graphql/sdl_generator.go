package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/mycel-labs/mycel/internal/validate"
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

// SetParsedSchema sets the parsed schema from SDL.
func (g *SDLGenerator) SetParsedSchema(schema *ParsedSchema) {
	g.parsedSchema = schema
}

// SetHCLConverter sets the HCL converter.
func (g *SDLGenerator) SetHCLConverter(converter *HCLConverter) {
	g.hclConverter = converter
}

// SetSDLConverter sets the SDL converter.
func (g *SDLGenerator) SetSDLConverter(converter *SDLConverter) {
	g.sdlConverter = converter
}

// SetQueryFields sets the Query fields.
func (g *SDLGenerator) SetQueryFields(fields graphql.Fields) {
	g.queryFields = fields
}

// SetMutationFields sets the Mutation fields.
func (g *SDLGenerator) SetMutationFields(fields graphql.Fields) {
	g.mutationFields = fields
}

// SetSubscriptionFields sets the Subscription fields.
func (g *SDLGenerator) SetSubscriptionFields(fields graphql.Fields) {
	g.subscriptionFields = fields
}

// SetFederation sets Federation support.
func (g *SDLGenerator) SetFederation(federation *FederationSupport) {
	g.federation = federation
}

// AddCustomScalar adds a custom scalar to include.
func (g *SDLGenerator) AddCustomScalar(name string) {
	g.customScalars = append(g.customScalars, name)
}

// SetIncludeDescriptions sets whether to include descriptions.
func (g *SDLGenerator) SetIncludeDescriptions(include bool) {
	g.includeDescriptions = include
}

// Generate generates the complete SDL.
func (g *SDLGenerator) Generate() string {
	var sb strings.Builder

	// 1. Federation schema extension if enabled
	if g.federation != nil {
		sb.WriteString(GetFederationDirectives(g.federation.version))
		sb.WriteString("\n\n")
	}

	// 2. Custom scalar definitions
	g.writeScalars(&sb)

	// 3. Enum definitions from HCL or parsed schema
	g.writeEnums(&sb)

	// 4. Input type definitions
	g.writeInputTypes(&sb)

	// 5. Interface definitions
	g.writeInterfaces(&sb)

	// 6. Type definitions (object types)
	g.writeObjectTypes(&sb)

	// 7. Union definitions
	g.writeUnions(&sb)

	// 8. Query type
	g.writeQueryType(&sb)

	// 9. Mutation type
	g.writeMutationType(&sb)

	// 10. Subscription type
	g.writeSubscriptionType(&sb)

	return sb.String()
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

// writeScalars writes scalar definitions.
func (g *SDLGenerator) writeScalars(sb *strings.Builder) {
	// Default scalars
	defaultScalars := []string{"JSON", "DateTime", "Date", "Time"}

	// Combine with custom scalars
	allScalars := make(map[string]bool)
	for _, s := range defaultScalars {
		allScalars[s] = true
	}
	for _, s := range g.customScalars {
		allScalars[s] = true
	}

	// Add scalars from parsed schema
	if g.parsedSchema != nil {
		for _, s := range g.parsedSchema.Scalars {
			allScalars[s] = true
		}
	}

	// Sort for deterministic output
	scalarNames := make([]string, 0, len(allScalars))
	for name := range allScalars {
		scalarNames = append(scalarNames, name)
	}
	sort.Strings(scalarNames)

	// Write scalar definitions
	for _, name := range scalarNames {
		if g.includeDescriptions {
			desc := getScalarDescription(name)
			if desc != "" {
				sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", desc))
			}
		}
		sb.WriteString(fmt.Sprintf("scalar %s\n", name))
	}
	if len(scalarNames) > 0 {
		sb.WriteString("\n")
	}
}

// writeEnums writes enum definitions.
func (g *SDLGenerator) writeEnums(sb *strings.Builder) {
	// Collect enums from HCL converter
	if g.hclConverter != nil {
		for name, enum := range g.hclConverter.AllEnums() {
			g.writeEnum(sb, name, enum)
		}
	}

	// Collect enums from parsed schema
	if g.parsedSchema != nil {
		for name, enumDef := range g.parsedSchema.Enums {
			g.writeParsedEnum(sb, name, enumDef)
		}
	}
}

// writeEnum writes a single enum definition.
func (g *SDLGenerator) writeEnum(sb *strings.Builder, name string, enum *graphql.Enum) {
	if g.includeDescriptions && enum.Description() != "" {
		sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", enum.Description()))
	}
	sb.WriteString(fmt.Sprintf("enum %s {\n", name))
	for _, val := range enum.Values() {
		sb.WriteString(fmt.Sprintf("  %s\n", val.Name))
	}
	sb.WriteString("}\n\n")
}

// writeParsedEnum writes an enum from ParsedEnum.
func (g *SDLGenerator) writeParsedEnum(sb *strings.Builder, name string, enum *ParsedEnum) {
	if g.includeDescriptions && enum.Description != "" {
		sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", enum.Description))
	}
	sb.WriteString(fmt.Sprintf("enum %s {\n", name))

	// Sort values for deterministic output
	valueNames := make([]string, 0, len(enum.Values))
	for name := range enum.Values {
		valueNames = append(valueNames, name)
	}
	sort.Strings(valueNames)

	for _, valName := range valueNames {
		sb.WriteString(fmt.Sprintf("  %s\n", valName))
	}
	sb.WriteString("}\n\n")
}

// writeInputTypes writes input type definitions.
func (g *SDLGenerator) writeInputTypes(sb *strings.Builder) {
	// From HCL converter
	if g.hclConverter != nil {
		for name, input := range g.hclConverter.AllInputs() {
			g.writeInputObject(sb, name, input)
		}
	}

	// From parsed schema
	if g.parsedSchema != nil {
		for name, inputDef := range g.parsedSchema.Inputs {
			g.writeParsedInput(sb, name, inputDef)
		}
	}
}

// writeInputObject writes a graphql.InputObject to SDL.
func (g *SDLGenerator) writeInputObject(sb *strings.Builder, name string, input *graphql.InputObject) {
	if g.includeDescriptions && input.Description() != "" {
		sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", input.Description()))
	}
	sb.WriteString(fmt.Sprintf("input %s {\n", name))

	// Sort fields for deterministic output
	fieldNames := make([]string, 0, len(input.Fields()))
	for fieldName := range input.Fields() {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		field := input.Fields()[fieldName]
		typeStr := formatGraphQLType(field.Type)
		sb.WriteString(fmt.Sprintf("  %s: %s\n", fieldName, typeStr))
	}
	sb.WriteString("}\n\n")
}

// writeParsedInput writes a ParsedInput to SDL.
func (g *SDLGenerator) writeParsedInput(sb *strings.Builder, name string, input *ParsedInput) {
	if g.includeDescriptions && input.Description != "" {
		sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", input.Description))
	}
	sb.WriteString(fmt.Sprintf("input %s {\n", name))

	// Sort fields
	fieldNames := make([]string, 0, len(input.Fields))
	for fieldName := range input.Fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		field := input.Fields[fieldName]
		typeStr := field.Type.String()
		sb.WriteString(fmt.Sprintf("  %s: %s\n", fieldName, typeStr))
	}
	sb.WriteString("}\n\n")
}

// writeInterfaces writes interface definitions.
func (g *SDLGenerator) writeInterfaces(sb *strings.Builder) {
	if g.parsedSchema == nil {
		return
	}

	for name, iface := range g.parsedSchema.Interfaces {
		if g.includeDescriptions && iface.Description != "" {
			sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", iface.Description))
		}
		sb.WriteString(fmt.Sprintf("interface %s {\n", name))

		// Sort fields
		fieldNames := make([]string, 0, len(iface.Fields))
		for fieldName := range iface.Fields {
			fieldNames = append(fieldNames, fieldName)
		}
		sort.Strings(fieldNames)

		for _, fieldName := range fieldNames {
			field := iface.Fields[fieldName]
			g.writeField(sb, fieldName, field)
		}
		sb.WriteString("}\n\n")
	}
}

// writeObjectTypes writes object type definitions.
func (g *SDLGenerator) writeObjectTypes(sb *strings.Builder) {
	// Collect all types
	allTypes := make(map[string]interface{})

	// From HCL converter
	if g.hclConverter != nil {
		for name, t := range g.hclConverter.AllTypes() {
			allTypes[name] = t
		}
	}

	// From parsed schema
	if g.parsedSchema != nil {
		for name, t := range g.parsedSchema.Types {
			allTypes[name] = t
		}
	}

	// Sort type names
	typeNames := make([]string, 0, len(allTypes))
	for name := range allTypes {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)

	// Write types
	for _, name := range typeNames {
		t := allTypes[name]
		switch typ := t.(type) {
		case *graphql.Object:
			g.writeGraphQLObject(sb, name, typ)
		case *ParsedType:
			g.writeParsedType(sb, name, typ)
		}
	}
}

// writeGraphQLObject writes a graphql.Object to SDL.
func (g *SDLGenerator) writeGraphQLObject(sb *strings.Builder, name string, obj *graphql.Object) {
	if g.includeDescriptions && obj.Description() != "" {
		sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", obj.Description()))
	}

	sb.WriteString(fmt.Sprintf("type %s", name))

	// Check for Federation @key directive
	if g.federation != nil {
		for _, entityName := range g.federation.GetEntityNames() {
			if entityName == name {
				sb.WriteString(" @key(fields: \"id\")")
				break
			}
		}
	}

	// Interfaces
	interfaces := obj.Interfaces()
	if len(interfaces) > 0 {
		sb.WriteString(" implements ")
		for i, iface := range interfaces {
			if i > 0 {
				sb.WriteString(" & ")
			}
			sb.WriteString(iface.Name())
		}
	}

	sb.WriteString(" {\n")

	// Sort fields
	fieldNames := make([]string, 0, len(obj.Fields()))
	for fieldName := range obj.Fields() {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		field := obj.Fields()[fieldName]
		typeStr := formatGraphQLType(field.Type)

		if g.includeDescriptions && field.Description != "" {
			sb.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", field.Description))
		}

		// Field with arguments (field.Args is []*Argument, a slice)
		if len(field.Args) > 0 {
			sb.WriteString(fmt.Sprintf("  %s(", fieldName))
			first := true
			// Sort args by name for deterministic output
			argNames := make([]string, 0, len(field.Args))
			argMap := make(map[string]*graphql.Argument)
			for _, arg := range field.Args {
				argNames = append(argNames, arg.PrivateName)
				argMap[arg.PrivateName] = arg
			}
			sort.Strings(argNames)
			for _, argName := range argNames {
				arg := argMap[argName]
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s: %s", argName, formatGraphQLType(arg.Type)))
				first = false
			}
			sb.WriteString(fmt.Sprintf("): %s\n", typeStr))
		} else {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", fieldName, typeStr))
		}
	}

	sb.WriteString("}\n\n")
}

// writeParsedType writes a ParsedType to SDL.
func (g *SDLGenerator) writeParsedType(sb *strings.Builder, name string, typ *ParsedType) {
	if g.includeDescriptions && typ.Description != "" {
		sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", typ.Description))
	}

	sb.WriteString(fmt.Sprintf("type %s", name))

	// Directives
	for _, dir := range typ.Directives {
		sb.WriteString(fmt.Sprintf(" @%s", dir.Name))
		if len(dir.Args) > 0 {
			sb.WriteString("(")
			first := true
			for argName, argVal := range dir.Args {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s: %q", argName, fmt.Sprint(argVal)))
				first = false
			}
			sb.WriteString(")")
		}
	}

	// Implements
	if len(typ.Implements) > 0 {
		sb.WriteString(" implements ")
		for i, iface := range typ.Implements {
			if i > 0 {
				sb.WriteString(" & ")
			}
			sb.WriteString(iface)
		}
	}

	sb.WriteString(" {\n")

	// Sort fields
	fieldNames := make([]string, 0, len(typ.Fields))
	for fieldName := range typ.Fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		field := typ.Fields[fieldName]
		g.writeField(sb, fieldName, field)
	}

	sb.WriteString("}\n\n")
}

// writeField writes a ParsedField to SDL.
func (g *SDLGenerator) writeField(sb *strings.Builder, name string, field *ParsedField) {
	if g.includeDescriptions && field.Description != "" {
		sb.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", field.Description))
	}

	sb.WriteString(fmt.Sprintf("  %s", name))

	// Arguments
	if len(field.Args) > 0 {
		sb.WriteString("(")
		first := true
		argNames := make([]string, 0, len(field.Args))
		for argName := range field.Args {
			argNames = append(argNames, argName)
		}
		sort.Strings(argNames)
		for _, argName := range argNames {
			arg := field.Args[argName]
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s: %s", argName, arg.Type.String()))
			first = false
		}
		sb.WriteString(")")
	}

	sb.WriteString(fmt.Sprintf(": %s", field.Type.String()))

	// Directives
	for _, dir := range field.Directives {
		sb.WriteString(fmt.Sprintf(" @%s", dir.Name))
		if len(dir.Args) > 0 {
			sb.WriteString("(")
			first := true
			for argName, argVal := range dir.Args {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s: %q", argName, fmt.Sprint(argVal)))
				first = false
			}
			sb.WriteString(")")
		}
	}

	sb.WriteString("\n")
}

// writeUnions writes union definitions.
func (g *SDLGenerator) writeUnions(sb *strings.Builder) {
	if g.parsedSchema == nil {
		return
	}

	for name, union := range g.parsedSchema.Unions {
		if g.includeDescriptions && union.Description != "" {
			sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", union.Description))
		}
		sb.WriteString(fmt.Sprintf("union %s = %s\n\n", name, strings.Join(union.Types, " | ")))
	}
}

// writeQueryType writes the Query type.
func (g *SDLGenerator) writeQueryType(sb *strings.Builder) {
	if len(g.queryFields) == 0 {
		return
	}

	sb.WriteString("type Query {\n")

	// Sort fields
	fieldNames := make([]string, 0, len(g.queryFields))
	for name := range g.queryFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		// Skip Federation internal fields
		if name == "_service" || name == "_entities" {
			continue
		}
		field := g.queryFields[name]
		g.writeGraphQLField(sb, name, field)
	}

	sb.WriteString("}\n\n")
}

// writeMutationType writes the Mutation type.
func (g *SDLGenerator) writeMutationType(sb *strings.Builder) {
	if len(g.mutationFields) == 0 {
		return
	}

	sb.WriteString("type Mutation {\n")

	// Sort fields
	fieldNames := make([]string, 0, len(g.mutationFields))
	for name := range g.mutationFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		field := g.mutationFields[name]
		g.writeGraphQLField(sb, name, field)
	}

	sb.WriteString("}\n\n")
}

// writeSubscriptionType writes the Subscription type.
func (g *SDLGenerator) writeSubscriptionType(sb *strings.Builder) {
	if len(g.subscriptionFields) == 0 {
		return
	}

	sb.WriteString("type Subscription {\n")

	// Sort fields
	fieldNames := make([]string, 0, len(g.subscriptionFields))
	for name := range g.subscriptionFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		field := g.subscriptionFields[name]
		g.writeGraphQLField(sb, name, field)
	}

	sb.WriteString("}\n\n")
}

// writeGraphQLField writes a graphql.Field to SDL.
func (g *SDLGenerator) writeGraphQLField(sb *strings.Builder, name string, field *graphql.Field) {
	if g.includeDescriptions && field.Description != "" {
		sb.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", field.Description))
	}

	sb.WriteString(fmt.Sprintf("  %s", name))

	// Arguments
	if len(field.Args) > 0 {
		sb.WriteString("(")
		first := true
		argNames := make([]string, 0, len(field.Args))
		for argName := range field.Args {
			argNames = append(argNames, argName)
		}
		sort.Strings(argNames)
		for _, argName := range argNames {
			arg := field.Args[argName]
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s: %s", argName, formatGraphQLType(arg.Type)))
			first = false
		}
		sb.WriteString(")")
	}

	sb.WriteString(fmt.Sprintf(": %s\n", formatGraphQLType(field.Type)))
}

// getScalarDescription returns a description for built-in custom scalars.
func getScalarDescription(name string) string {
	switch name {
	case "JSON":
		return "Arbitrary JSON data"
	case "DateTime":
		return "ISO 8601 date-time string"
	case "Date":
		return "Date in YYYY-MM-DD format"
	case "Time":
		return "Time in HH:MM:SS format"
	default:
		return ""
	}
}

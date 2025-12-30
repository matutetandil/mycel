package graphql

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/graphql-go/graphql"
	"github.com/mycel-labs/mycel/internal/validate"
)

// SchemaMode defines how the schema is built.
type SchemaMode string

const (
	// SchemaModeAuto automatically detects based on what's loaded.
	SchemaModeAuto SchemaMode = "auto"

	// SchemaModeSDL builds schema from SDL file (schema-first).
	SchemaModeSDL SchemaMode = "sdl"

	// SchemaModeHCL builds schema from HCL types (HCL-first).
	SchemaModeHCL SchemaMode = "hcl"

	// SchemaModeHybrid combines SDL and HCL types.
	SchemaModeHybrid SchemaMode = "hybrid"

	// SchemaModeDynamic creates fields dynamically as handlers register.
	SchemaModeDynamic SchemaMode = "dynamic"
)

// SchemaBuilder builds GraphQL schemas from SDL files, HCL types, or dynamically.
type SchemaBuilder struct {
	mu sync.RWMutex

	// mode determines how the schema is built.
	mode SchemaMode

	// Dynamic mode fields
	queryFields    graphql.Fields
	mutationFields graphql.Fields
	types          map[string]*graphql.Object

	// Schema-first mode fields
	parsedSchema *ParsedSchema
	sdlConverter *SDLConverter

	// HCL-first mode fields
	hclConverter *HCLConverter

	// Handlers map "Type.field" to their handlers
	handlers map[string]HandlerFunc

	// Federation support
	federation *FederationSupport

	// Raw SDL for reference
	rawSDL string
}

// NewSchemaBuilder creates a new schema builder.
func NewSchemaBuilder() *SchemaBuilder {
	return &SchemaBuilder{
		mode:           SchemaModeAuto,
		queryFields:    make(graphql.Fields),
		mutationFields: make(graphql.Fields),
		types:          make(map[string]*graphql.Object),
		handlers:       make(map[string]HandlerFunc),
	}
}

// SetMode sets the schema building mode.
func (b *SchemaBuilder) SetMode(mode SchemaMode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mode = mode
}

// GetMode returns the current schema mode.
func (b *SchemaBuilder) GetMode() SchemaMode {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.mode
}

// LoadSDL loads and parses a GraphQL SDL file.
// This enables schema-first mode where types are defined in SDL.
func (b *SchemaBuilder) LoadSDL(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read SDL file: %w", err)
	}

	return b.ParseSDL(string(content))
}

// ParseSDL parses GraphQL SDL and builds types from it.
// After parsing, fields defined in SDL will use proper types.
func (b *SchemaBuilder) ParseSDL(sdl string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Use the new AST parser
	parsed, err := ParseSDLComplete(sdl)
	if err != nil {
		return fmt.Errorf("failed to parse SDL: %w", err)
	}

	b.parsedSchema = parsed
	b.rawSDL = sdl

	// Convert to graphql-go types
	b.sdlConverter = NewSDLConverter()
	if err := b.sdlConverter.Convert(parsed); err != nil {
		return fmt.Errorf("failed to convert SDL: %w", err)
	}

	// Store converted types
	for name, gqlType := range b.sdlConverter.AllTypes() {
		b.types[name] = gqlType
	}

	// Create Query fields from SDL with proper types (but no resolvers yet)
	if parsed.Query != nil {
		for fieldName, fieldDef := range parsed.Query.Fields {
			b.queryFields[fieldName] = b.createFieldFromParsed("Query", fieldName, fieldDef)
		}
	}

	// Create Mutation fields from SDL with proper types (but no resolvers yet)
	if parsed.Mutation != nil {
		for fieldName, fieldDef := range parsed.Mutation.Fields {
			b.mutationFields[fieldName] = b.createFieldFromParsed("Mutation", fieldName, fieldDef)
		}
	}

	// Set mode to SDL if not already set
	if b.mode == SchemaModeAuto {
		b.mode = SchemaModeSDL
	}

	return nil
}

// LoadHCLTypes loads type schemas from HCL and converts them to GraphQL types.
// This enables HCL-first mode where types are defined in HCL configuration.
func (b *SchemaBuilder) LoadHCLTypes(types map[string]*validate.TypeSchema) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(types) == 0 {
		return nil
	}

	// Create HCL converter if not exists
	if b.hclConverter == nil {
		b.hclConverter = NewHCLConverter()
	}

	// Load type schemas
	b.hclConverter.LoadTypeSchemas(types)

	// Convert to GraphQL types
	if err := b.hclConverter.Convert(); err != nil {
		return fmt.Errorf("failed to convert HCL types: %w", err)
	}

	// Store converted types
	for name, gqlType := range b.hclConverter.AllTypes() {
		b.types[name] = gqlType
	}

	// Set mode to HCL if not already set to SDL
	if b.mode == SchemaModeAuto {
		b.mode = SchemaModeHCL
	} else if b.mode == SchemaModeSDL {
		b.mode = SchemaModeHybrid
	}

	return nil
}

// GetHCLConverter returns the HCL converter.
func (b *SchemaBuilder) GetHCLConverter() *HCLConverter {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hclConverter
}

// createFieldFromParsed creates a graphql.Field from a ParsedField definition.
func (b *SchemaBuilder) createFieldFromParsed(typeName, fieldName string, def *ParsedField) *graphql.Field {
	field := &graphql.Field{
		Description: def.Description,
	}

	// Resolve the return type
	if b.sdlConverter != nil {
		field.Type = b.sdlConverter.resolveOutputType(def.Type)
	} else {
		// Fallback to JSON if no converter
		field.Type = JSONScalar
	}

	// Set deprecation if present
	if def.DeprecationReason != "" {
		field.DeprecationReason = def.DeprecationReason
	}

	// Convert arguments
	if len(def.Args) > 0 {
		field.Args = graphql.FieldConfigArgument{}
		for argName, argDef := range def.Args {
			var argType graphql.Input = graphql.String // Default
			if b.sdlConverter != nil {
				argType = b.sdlConverter.resolveInputType(argDef.Type)
			}
			field.Args[argName] = &graphql.ArgumentConfig{
				Type:         argType,
				Description:  argDef.Description,
				DefaultValue: argDef.DefaultValue,
			}
		}
	}

	// Set a default resolver that returns an error (will be overwritten when handler registers)
	operation := fmt.Sprintf("%s.%s", typeName, fieldName)
	field.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
		return nil, fmt.Errorf("no handler registered for %s", operation)
	}

	return field
}

// RegisterHandler registers a handler for a GraphQL operation.
// In SDL mode, this connects a handler to an existing field and uses smart resolving.
// In dynamic mode, this creates the field with generic JSON types.
func (b *SchemaBuilder) RegisterHandler(operation string, handler HandlerFunc) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	parts := strings.SplitN(operation, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid operation format: %s (expected Type.field)", operation)
	}

	typeName := parts[0]
	fieldName := parts[1]

	// Store the handler
	b.handlers[operation] = handler

	// Check if we're in SDL mode and the field already exists
	var fields graphql.Fields
	switch strings.ToLower(typeName) {
	case "query":
		fields = b.queryFields
	case "mutation":
		fields = b.mutationFields
	default:
		return fmt.Errorf("unsupported type: %s (expected Query or Mutation)", typeName)
	}

	// If field already exists (from SDL), use smart resolver that auto-detects return type
	if existingField, exists := fields[fieldName]; exists {
		// Use smart resolver that automatically unwraps single results for non-list types
		existingField.Resolve = CreateSmartResolver(handler)
		return nil
	}

	// Field doesn't exist - create it dynamically (dynamic mode)
	// Use basic resolver since we don't have type info
	resolver := CreateResolver(handler)

	var returnType graphql.Output
	switch strings.ToLower(typeName) {
	case "query":
		returnType = graphql.NewList(JSONScalar)
	case "mutation":
		returnType = JSONScalar
	}

	field := &graphql.Field{
		Type:        returnType,
		Description: fmt.Sprintf("Handler for %s", operation),
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        JSONScalar,
				Description: "Input arguments as JSON",
			},
		},
		Resolve: resolver,
	}

	fields[fieldName] = field

	// Update mode if needed
	if b.mode == SchemaModeAuto && b.parsedSchema == nil {
		b.mode = SchemaModeDynamic
	}

	return nil
}

// RegisterHandlerWithReturnType registers a handler with a specific return type.
// Use this when the return type is defined in HCL types.
func (b *SchemaBuilder) RegisterHandlerWithReturnType(operation string, handler HandlerFunc, returnType string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	parts := strings.SplitN(operation, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid operation format: %s (expected Type.field)", operation)
	}

	typeName := parts[0]
	fieldName := parts[1]

	b.handlers[operation] = handler
	// Use smart resolver to automatically unwrap single results for non-list types
	resolver := CreateSmartResolver(handler)

	// Resolve the return type
	gqlType := b.resolveReturnType(returnType)

	field := &graphql.Field{
		Type:        gqlType,
		Description: fmt.Sprintf("Handler for %s", operation),
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        JSONScalar,
				Description: "Input arguments as JSON",
			},
		},
		Resolve: resolver,
	}

	switch strings.ToLower(typeName) {
	case "query":
		b.queryFields[fieldName] = field
	case "mutation":
		b.mutationFields[fieldName] = field
	default:
		return fmt.Errorf("unsupported type: %s", typeName)
	}

	return nil
}

// resolveReturnType parses a return type string and returns the graphql.Output.
// Supports formats like: "User", "User[]", "[User]", "[User!]!", "User!"
func (b *SchemaBuilder) resolveReturnType(returnType string) graphql.Output {
	returnType = strings.TrimSpace(returnType)

	// Check for list syntax
	isList := false
	listNonNull := false
	elementNonNull := false

	// Handle "Type[]" syntax (simple list)
	if strings.HasSuffix(returnType, "[]") {
		isList = true
		listNonNull = true
		elementNonNull = true
		returnType = strings.TrimSuffix(returnType, "[]")
	}

	// Handle "[Type!]!" syntax
	if strings.HasPrefix(returnType, "[") {
		isList = true
		// Remove outer brackets
		returnType = strings.TrimPrefix(returnType, "[")

		if strings.HasSuffix(returnType, "]!") {
			listNonNull = true
			returnType = strings.TrimSuffix(returnType, "]!")
		} else if strings.HasSuffix(returnType, "]") {
			returnType = strings.TrimSuffix(returnType, "]")
		}

		// Check for element non-null
		if strings.HasSuffix(returnType, "!") {
			elementNonNull = true
			returnType = strings.TrimSuffix(returnType, "!")
		}
	}

	// Check for non-null on base type
	baseNonNull := false
	if strings.HasSuffix(returnType, "!") {
		baseNonNull = true
		returnType = strings.TrimSuffix(returnType, "!")
	}

	// Resolve base type
	var baseType graphql.Output
	switch returnType {
	case "ID":
		baseType = graphql.ID
	case "String":
		baseType = graphql.String
	case "Int":
		baseType = graphql.Int
	case "Float":
		baseType = graphql.Float
	case "Boolean":
		baseType = graphql.Boolean
	case "JSON":
		baseType = JSONScalar
	case "DateTime":
		baseType = DateTimeScalar
	case "Date":
		baseType = DateScalar
	case "Time":
		baseType = TimeScalar
	default:
		// Check custom types
		if t, exists := b.types[returnType]; exists {
			baseType = t
		} else if b.sdlConverter != nil {
			if t := b.sdlConverter.GetType(returnType); t != nil {
				baseType = t
			} else if e := b.sdlConverter.GetEnum(returnType); e != nil {
				baseType = e
			}
		}
		if baseType == nil {
			baseType = JSONScalar // Fallback
		}
	}

	// Apply modifiers
	if isList {
		var listType graphql.Output = baseType
		if elementNonNull {
			listType = graphql.NewNonNull(listType)
		}
		listType = graphql.NewList(listType)
		if listNonNull {
			listType = graphql.NewNonNull(listType)
		}
		return listType
	}

	if baseNonNull {
		return graphql.NewNonNull(baseType)
	}

	return baseType
}

// Build creates the GraphQL schema from all registered sources.
func (b *SchemaBuilder) Build() (*graphql.Schema, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	config := graphql.SchemaConfig{}

	// Copy query fields to avoid modifying the original
	queryFields := make(graphql.Fields)
	for k, v := range b.queryFields {
		queryFields[k] = v
	}

	// Add Federation fields if enabled
	if b.federation != nil {
		queryFields["_service"] = b.federation.CreateServiceField()
		queryFields["_entities"] = b.federation.CreateEntitiesField()

		// Generate and set the SDL
		sdl := b.generateSDL()
		b.federation.SetSDL(sdl)
	}

	// Create Query type
	if len(queryFields) > 0 {
		config.Query = graphql.NewObject(graphql.ObjectConfig{
			Name:   "Query",
			Fields: queryFields,
		})
	} else {
		config.Query = graphql.NewObject(graphql.ObjectConfig{
			Name: "Query",
			Fields: graphql.Fields{
				"_empty": &graphql.Field{
					Type: graphql.String,
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return nil, nil
					},
				},
			},
		})
	}

	// Create Mutation type
	if len(b.mutationFields) > 0 {
		config.Mutation = graphql.NewObject(graphql.ObjectConfig{
			Name:   "Mutation",
			Fields: b.mutationFields,
		})
	}

	schema, err := graphql.NewSchema(config)
	if err != nil {
		return nil, err
	}
	return &schema, nil
}

// EnableFederation enables Federation support.
func (b *SchemaBuilder) EnableFederation(version int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if version == 0 {
		version = 2
	}
	b.federation = NewFederationSupport(version)
}

// IsFederationEnabled returns true if Federation is enabled.
func (b *SchemaBuilder) IsFederationEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.federation != nil
}

// GetFederation returns the Federation support instance.
func (b *SchemaBuilder) GetFederation() *FederationSupport {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.federation
}

// RegisterEntity registers a federated entity type.
func (b *SchemaBuilder) RegisterEntity(typeName string, keys []EntityKey, resolver EntityResolver) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.federation == nil {
		return
	}

	gqlType := b.getOrCreateEntityType(typeName)
	b.federation.RegisterEntity(typeName, keys, resolver, gqlType)
}

// getOrCreateEntityType gets or creates a GraphQL type for an entity.
func (b *SchemaBuilder) getOrCreateEntityType(typeName string) *graphql.Object {
	// First check if we have it from SDL
	if t, exists := b.types[typeName]; exists {
		return t
	}

	// Check SDL converter
	if b.sdlConverter != nil {
		if t := b.sdlConverter.GetType(typeName); t != nil {
			b.types[typeName] = t
			return t
		}
	}

	// Create a generic type
	t := graphql.NewObject(graphql.ObjectConfig{
		Name: typeName,
		Fields: graphql.Fields{
			"_json": &graphql.Field{
				Type:        JSONScalar,
				Description: "Entity data as JSON",
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return p.Source, nil
				},
			},
		},
	})

	b.types[typeName] = t
	return t
}

// RegisterType registers a custom GraphQL object type.
func (b *SchemaBuilder) RegisterType(name string, gqlType *graphql.Object) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.types[name] = gqlType
}

// GetType returns a registered type by name.
func (b *SchemaBuilder) GetType(name string) *graphql.Object {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.types[name]
}

// GetParsedSchema returns the parsed SDL schema.
func (b *SchemaBuilder) GetParsedSchema() *ParsedSchema {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.parsedSchema
}

// GetSDLConverter returns the SDL converter.
func (b *SchemaBuilder) GetSDLConverter() *SDLConverter {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sdlConverter
}

// SetRawSDL sets the raw SDL string.
func (b *SchemaBuilder) SetRawSDL(sdl string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rawSDL = sdl
}

// GetRawSDL returns the raw SDL string.
func (b *SchemaBuilder) GetRawSDL() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.rawSDL
}

// UpdateResolver updates the resolver for an existing field.
func (b *SchemaBuilder) UpdateResolver(operation string, handler HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()

	parts := strings.SplitN(operation, ".", 2)
	if len(parts) != 2 {
		return
	}

	typeName := parts[0]
	fieldName := parts[1]
	b.handlers[operation] = handler

	var fields graphql.Fields
	switch strings.ToLower(typeName) {
	case "query":
		fields = b.queryFields
	case "mutation":
		fields = b.mutationFields
	default:
		return
	}

	if field, ok := fields[fieldName]; ok {
		field.Resolve = CreateResolver(handler)
	}
}

// HasField checks if a field exists.
func (b *SchemaBuilder) HasField(operation string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	parts := strings.SplitN(operation, ".", 2)
	if len(parts) != 2 {
		return false
	}

	typeName := parts[0]
	fieldName := parts[1]

	switch strings.ToLower(typeName) {
	case "query":
		_, exists := b.queryFields[fieldName]
		return exists
	case "mutation":
		_, exists := b.mutationFields[fieldName]
		return exists
	}

	return false
}

// generateSDL generates the complete SDL for the schema.
func (b *SchemaBuilder) generateSDL() string {
	var sb strings.Builder

	// Add Federation directives if enabled
	if b.federation != nil {
		sb.WriteString(GetFederationDirectives(b.federation.version))
		sb.WriteString("\n")
	}

	// If we have raw SDL from a file, use it
	if b.rawSDL != "" {
		sb.WriteString(b.rawSDL)
		return sb.String()
	}

	// Generate SDL from registered fields
	sb.WriteString("type Query {\n")
	for name, field := range b.queryFields {
		if name == "_service" || name == "_entities" {
			continue
		}
		sb.WriteString(fmt.Sprintf("  %s", name))
		if len(field.Args) > 0 {
			sb.WriteString("(")
			first := true
			for argName, arg := range field.Args {
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
	sb.WriteString("}\n\n")

	if len(b.mutationFields) > 0 {
		sb.WriteString("type Mutation {\n")
		for name, field := range b.mutationFields {
			sb.WriteString(fmt.Sprintf("  %s", name))
			if len(field.Args) > 0 {
				sb.WriteString("(")
				first := true
				for argName, arg := range field.Args {
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
		sb.WriteString("}\n\n")
	}

	// Add registered types with @key directives
	if b.federation != nil {
		for _, entityName := range b.federation.GetEntityNames() {
			sb.WriteString(fmt.Sprintf("type %s @key(fields: \"id\") {\n", entityName))
			sb.WriteString("  id: ID!\n")
			sb.WriteString("  _json: JSON\n")
			sb.WriteString("}\n\n")
		}
	}

	// Add scalar definitions
	sb.WriteString("scalar JSON\n")
	sb.WriteString("scalar DateTime\n")
	sb.WriteString("scalar Date\n")
	sb.WriteString("scalar Time\n")

	return sb.String()
}

// formatGraphQLType formats a graphql.Type to SDL string.
func formatGraphQLType(t graphql.Type) string {
	switch typ := t.(type) {
	case *graphql.NonNull:
		return fmt.Sprintf("%s!", formatGraphQLType(typ.OfType))
	case *graphql.List:
		return fmt.Sprintf("[%s]", formatGraphQLType(typ.OfType))
	case *graphql.Object:
		return typ.Name()
	case *graphql.Scalar:
		return typ.Name()
	case *graphql.Enum:
		return typ.Name()
	case *graphql.InputObject:
		return typ.Name()
	case *graphql.Interface:
		return typ.Name()
	case *graphql.Union:
		return typ.Name()
	default:
		return "JSON"
	}
}

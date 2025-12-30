package graphql

import (
	"fmt"
	"sync"

	"github.com/graphql-go/graphql"
)

// SDLConverter converts a ParsedSchema to graphql-go types.
type SDLConverter struct {
	mu sync.RWMutex

	// types holds converted object types.
	types map[string]*graphql.Object

	// inputs holds converted input object types.
	inputs map[string]*graphql.InputObject

	// enums holds converted enum types.
	enums map[string]*graphql.Enum

	// interfaces holds converted interface types.
	interfaces map[string]*graphql.Interface

	// unions holds converted union types.
	unions map[string]*graphql.Union

	// scalars holds custom scalar types.
	scalars map[string]*graphql.Scalar

	// parsedSchema is the source schema.
	parsedSchema *ParsedSchema

	// fieldResolvers maps "TypeName.fieldName" to resolvers.
	fieldResolvers map[string]graphql.FieldResolveFn
}

// NewSDLConverter creates a new SDL converter.
func NewSDLConverter() *SDLConverter {
	return &SDLConverter{
		types:          make(map[string]*graphql.Object),
		inputs:         make(map[string]*graphql.InputObject),
		enums:          make(map[string]*graphql.Enum),
		interfaces:     make(map[string]*graphql.Interface),
		unions:         make(map[string]*graphql.Union),
		scalars:        make(map[string]*graphql.Scalar),
		fieldResolvers: make(map[string]graphql.FieldResolveFn),
	}
}

// Convert converts a ParsedSchema to graphql-go types.
func (c *SDLConverter) Convert(parsed *ParsedSchema) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.parsedSchema = parsed

	// Register custom scalars first
	c.registerBuiltinScalars()
	for _, scalarName := range parsed.Scalars {
		c.createCustomScalar(scalarName)
	}

	// Convert enums (no dependencies)
	for name, enumDef := range parsed.Enums {
		c.enums[name] = c.convertEnum(enumDef)
	}

	// Convert interfaces (may reference scalars, enums)
	for name, ifaceDef := range parsed.Interfaces {
		c.interfaces[name] = c.convertInterface(ifaceDef)
	}

	// Convert input types (may reference scalars, enums, other inputs)
	for name, inputDef := range parsed.Inputs {
		c.inputs[name] = c.convertInput(inputDef)
	}

	// Create placeholder objects first (for circular references)
	for name := range parsed.Types {
		c.types[name] = graphql.NewObject(graphql.ObjectConfig{
			Name:   name,
			Fields: graphql.Fields{},
		})
	}

	// Now fill in the fields
	for name, typeDef := range parsed.Types {
		c.fillObjectFields(name, typeDef)
	}

	// Convert unions (now all types exist)
	for name, unionDef := range parsed.Unions {
		c.unions[name] = c.convertUnion(unionDef)
	}

	return nil
}

// registerBuiltinScalars registers the built-in custom scalars.
func (c *SDLConverter) registerBuiltinScalars() {
	c.scalars["JSON"] = JSONScalar
	c.scalars["DateTime"] = DateTimeScalar
	c.scalars["Date"] = DateScalar
	c.scalars["Time"] = TimeScalar
}

// createCustomScalar creates a passthrough scalar for unknown types.
func (c *SDLConverter) createCustomScalar(name string) {
	if _, exists := c.scalars[name]; exists {
		return
	}

	c.scalars[name] = graphql.NewScalar(graphql.ScalarConfig{
		Name:        name,
		Description: fmt.Sprintf("Custom scalar type %s", name),
		Serialize: func(value interface{}) interface{} {
			return value
		},
		ParseValue: func(value interface{}) interface{} {
			return value
		},
		ParseLiteral: parseLiteralValue,
	})
}

// convertEnum converts a ParsedEnum to graphql.Enum.
func (c *SDLConverter) convertEnum(def *ParsedEnum) *graphql.Enum {
	values := graphql.EnumValueConfigMap{}

	for name, val := range def.Values {
		config := &graphql.EnumValueConfig{
			Value:       name,
			Description: val.Description,
		}
		if val.DeprecationReason != "" {
			config.DeprecationReason = val.DeprecationReason
		}
		values[name] = config
	}

	return graphql.NewEnum(graphql.EnumConfig{
		Name:        def.Name,
		Description: def.Description,
		Values:      values,
	})
}

// convertInterface converts a ParsedInterface to graphql.Interface.
func (c *SDLConverter) convertInterface(def *ParsedInterface) *graphql.Interface {
	return graphql.NewInterface(graphql.InterfaceConfig{
		Name:        def.Name,
		Description: def.Description,
		Fields: graphql.FieldsThunk(func() graphql.Fields {
			fields := graphql.Fields{}
			for name, fieldDef := range def.Fields {
				fields[name] = c.convertField(def.Name, fieldDef)
			}
			return fields
		}),
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			// Default implementation - can be overridden
			if m, ok := p.Value.(map[string]interface{}); ok {
				if typeName, ok := m["__typename"].(string); ok {
					return c.types[typeName]
				}
			}
			return nil
		},
	})
}

// convertInput converts a ParsedInput to graphql.InputObject.
func (c *SDLConverter) convertInput(def *ParsedInput) *graphql.InputObject {
	return graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        def.Name,
		Description: def.Description,
		Fields: graphql.InputObjectConfigFieldMapThunk(func() graphql.InputObjectConfigFieldMap {
			fields := graphql.InputObjectConfigFieldMap{}
			for name, fieldDef := range def.Fields {
				fields[name] = c.convertInputField(fieldDef)
			}
			return fields
		}),
	})
}

// convertInputField converts a ParsedArg to input field config.
func (c *SDLConverter) convertInputField(def *ParsedArg) *graphql.InputObjectFieldConfig {
	return &graphql.InputObjectFieldConfig{
		Type:         c.resolveInputType(def.Type),
		Description:  def.Description,
		DefaultValue: def.DefaultValue,
	}
}

// fillObjectFields fills in the fields for an object type.
func (c *SDLConverter) fillObjectFields(name string, def *ParsedType) {
	obj := c.types[name]
	if obj == nil {
		return
	}

	// Get interfaces
	var interfaces []*graphql.Interface
	for _, ifaceName := range def.Implements {
		if iface, ok := c.interfaces[ifaceName]; ok {
			interfaces = append(interfaces, iface)
		}
	}

	// Create new object with fields
	fields := graphql.Fields{}
	for fieldName, fieldDef := range def.Fields {
		fields[fieldName] = c.convertField(name, fieldDef)
	}

	// Replace the placeholder
	newObj := graphql.NewObject(graphql.ObjectConfig{
		Name:        name,
		Description: def.Description,
		Fields:      fields,
		Interfaces:  interfaces,
	})

	c.types[name] = newObj
}

// convertField converts a ParsedField to graphql.Field.
func (c *SDLConverter) convertField(typeName string, def *ParsedField) *graphql.Field {
	field := &graphql.Field{
		Type:        c.resolveOutputType(def.Type),
		Description: def.Description,
	}

	if def.DeprecationReason != "" {
		field.DeprecationReason = def.DeprecationReason
	}

	// Convert arguments
	if len(def.Args) > 0 {
		field.Args = graphql.FieldConfigArgument{}
		for argName, argDef := range def.Args {
			field.Args[argName] = &graphql.ArgumentConfig{
				Type:         c.resolveInputType(argDef.Type),
				Description:  argDef.Description,
				DefaultValue: argDef.DefaultValue,
			}
		}
	}

	// Check for registered resolver
	resolverKey := fmt.Sprintf("%s.%s", typeName, def.Name)
	if resolver, ok := c.fieldResolvers[resolverKey]; ok {
		field.Resolve = resolver
	}

	return field
}

// convertUnion converts a ParsedUnion to graphql.Union.
func (c *SDLConverter) convertUnion(def *ParsedUnion) *graphql.Union {
	var types []*graphql.Object
	for _, typeName := range def.Types {
		if t, ok := c.types[typeName]; ok {
			types = append(types, t)
		}
	}

	return graphql.NewUnion(graphql.UnionConfig{
		Name:        def.Name,
		Description: def.Description,
		Types:       types,
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			if m, ok := p.Value.(map[string]interface{}); ok {
				if typeName, ok := m["__typename"].(string); ok {
					return c.types[typeName]
				}
			}
			return nil
		},
	})
}

// resolveOutputType resolves a ParsedTypeRef to a graphql.Output type.
func (c *SDLConverter) resolveOutputType(ref *ParsedTypeRef) graphql.Output {
	baseType := c.resolveBaseType(ref.Name)

	if ref.IsList {
		var listType graphql.Output = baseType
		if ref.ElementNonNull {
			listType = graphql.NewNonNull(listType)
		}
		listType = graphql.NewList(listType)
		if ref.ListNonNull {
			listType = graphql.NewNonNull(listType)
		}
		return listType
	}

	if ref.NonNull {
		return graphql.NewNonNull(baseType)
	}

	return baseType
}

// resolveInputType resolves a ParsedTypeRef to a graphql.Input type.
func (c *SDLConverter) resolveInputType(ref *ParsedTypeRef) graphql.Input {
	baseType := c.resolveBaseInputType(ref.Name)

	if ref.IsList {
		var listType graphql.Input = baseType
		if ref.ElementNonNull {
			listType = graphql.NewNonNull(listType)
		}
		listType = graphql.NewList(listType)
		if ref.ListNonNull {
			listType = graphql.NewNonNull(listType)
		}
		return listType
	}

	if ref.NonNull {
		return graphql.NewNonNull(baseType)
	}

	return baseType
}

// resolveBaseType resolves a type name to its graphql.Output type.
func (c *SDLConverter) resolveBaseType(name string) graphql.Output {
	// Check built-in scalars first
	switch name {
	case "ID":
		return graphql.ID
	case "String":
		return graphql.String
	case "Int":
		return graphql.Int
	case "Float":
		return graphql.Float
	case "Boolean":
		return graphql.Boolean
	}

	// Check custom scalars
	if scalar, ok := c.scalars[name]; ok {
		return scalar
	}

	// Check enums
	if enum, ok := c.enums[name]; ok {
		return enum
	}

	// Check interfaces
	if iface, ok := c.interfaces[name]; ok {
		return iface
	}

	// Check unions
	if union, ok := c.unions[name]; ok {
		return union
	}

	// Check object types
	if obj, ok := c.types[name]; ok {
		return obj
	}

	// Fallback to String for unknown types
	return graphql.String
}

// resolveBaseInputType resolves a type name to its graphql.Input type.
func (c *SDLConverter) resolveBaseInputType(name string) graphql.Input {
	// Check built-in scalars first
	switch name {
	case "ID":
		return graphql.ID
	case "String":
		return graphql.String
	case "Int":
		return graphql.Int
	case "Float":
		return graphql.Float
	case "Boolean":
		return graphql.Boolean
	}

	// Check custom scalars
	if scalar, ok := c.scalars[name]; ok {
		return scalar
	}

	// Check enums
	if enum, ok := c.enums[name]; ok {
		return enum
	}

	// Check input types
	if input, ok := c.inputs[name]; ok {
		return input
	}

	// Fallback to String for unknown types
	return graphql.String
}

// GetType returns a converted type by name.
func (c *SDLConverter) GetType(name string) *graphql.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.types[name]
}

// GetInput returns a converted input type by name.
func (c *SDLConverter) GetInput(name string) *graphql.InputObject {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inputs[name]
}

// GetEnum returns a converted enum by name.
func (c *SDLConverter) GetEnum(name string) *graphql.Enum {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enums[name]
}

// GetInterface returns a converted interface by name.
func (c *SDLConverter) GetInterface(name string) *graphql.Interface {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.interfaces[name]
}

// GetUnion returns a converted union by name.
func (c *SDLConverter) GetUnion(name string) *graphql.Union {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.unions[name]
}

// GetScalar returns a custom scalar by name.
func (c *SDLConverter) GetScalar(name string) *graphql.Scalar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.scalars[name]
}

// RegisterResolver registers a field resolver.
func (c *SDLConverter) RegisterResolver(typeName, fieldName string, resolver graphql.FieldResolveFn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := fmt.Sprintf("%s.%s", typeName, fieldName)
	c.fieldResolvers[key] = resolver
}

// GetQueryFields returns fields for building the Query type.
func (c *SDLConverter) GetQueryFields() graphql.Fields {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.parsedSchema == nil || c.parsedSchema.Query == nil {
		return nil
	}

	fields := graphql.Fields{}
	for name, fieldDef := range c.parsedSchema.Query.Fields {
		fields[name] = c.convertField("Query", fieldDef)
	}
	return fields
}

// GetMutationFields returns fields for building the Mutation type.
func (c *SDLConverter) GetMutationFields() graphql.Fields {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.parsedSchema == nil || c.parsedSchema.Mutation == nil {
		return nil
	}

	fields := graphql.Fields{}
	for name, fieldDef := range c.parsedSchema.Mutation.Fields {
		fields[name] = c.convertField("Mutation", fieldDef)
	}
	return fields
}

// AllTypes returns all converted object types.
func (c *SDLConverter) AllTypes() map[string]*graphql.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*graphql.Object, len(c.types))
	for k, v := range c.types {
		result[k] = v
	}
	return result
}

// AllInputs returns all converted input types.
func (c *SDLConverter) AllInputs() map[string]*graphql.InputObject {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*graphql.InputObject, len(c.inputs))
	for k, v := range c.inputs {
		result[k] = v
	}
	return result
}

// AllEnums returns all converted enum types.
func (c *SDLConverter) AllEnums() map[string]*graphql.Enum {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*graphql.Enum, len(c.enums))
	for k, v := range c.enums {
		result[k] = v
	}
	return result
}

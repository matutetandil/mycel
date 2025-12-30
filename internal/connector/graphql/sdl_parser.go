package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
)

// ParsedSchema contains the fully parsed SDL schema.
type ParsedSchema struct {
	// Types maps type names to their definitions.
	Types map[string]*ParsedType

	// Enums maps enum names to their definitions.
	Enums map[string]*ParsedEnum

	// Inputs maps input type names to their definitions.
	Inputs map[string]*ParsedInput

	// Interfaces maps interface names to their definitions.
	Interfaces map[string]*ParsedInterface

	// Unions maps union names to their definitions.
	Unions map[string]*ParsedUnion

	// Scalars contains custom scalar names.
	Scalars []string

	// Query is the Query type definition (if explicitly defined).
	Query *ParsedType

	// Mutation is the Mutation type definition (if explicitly defined).
	Mutation *ParsedType

	// Subscription is the Subscription type definition (if explicitly defined).
	Subscription *ParsedType

	// Directives contains directive definitions.
	Directives map[string]*ParsedDirectiveDef
}

// ParsedType represents a GraphQL object type.
type ParsedType struct {
	// Name is the type name.
	Name string

	// Description is the type description.
	Description string

	// Fields maps field names to their definitions.
	Fields map[string]*ParsedField

	// Implements lists interface names this type implements.
	Implements []string

	// Directives applied to this type.
	Directives []*ParsedDirective

	// IsExtension indicates if this is a type extension.
	IsExtension bool
}

// ParsedField represents a field in a type.
type ParsedField struct {
	// Name is the field name.
	Name string

	// Description is the field description.
	Description string

	// Type is the field's type reference.
	Type *ParsedTypeRef

	// Args maps argument names to their definitions.
	Args map[string]*ParsedArg

	// Directives applied to this field.
	Directives []*ParsedDirective

	// DeprecationReason if the field is deprecated.
	DeprecationReason string
}

// ParsedArg represents a field argument or input field.
type ParsedArg struct {
	// Name is the argument name.
	Name string

	// Description is the argument description.
	Description string

	// Type is the argument's type reference.
	Type *ParsedTypeRef

	// DefaultValue is the default value (if any).
	DefaultValue interface{}

	// Directives applied to this argument.
	Directives []*ParsedDirective
}

// ParsedTypeRef represents a type reference (e.g., String!, [User!]!).
type ParsedTypeRef struct {
	// Name is the base type name (e.g., "String", "User").
	Name string

	// IsList indicates if this is a list type.
	IsList bool

	// NonNull indicates if the type itself is non-null.
	NonNull bool

	// ListNonNull indicates if the list wrapper is non-null (for [Type]!).
	ListNonNull bool

	// ElementNonNull indicates if list elements are non-null (for [Type!]).
	ElementNonNull bool
}

// ParsedEnum represents an enum type.
type ParsedEnum struct {
	// Name is the enum name.
	Name string

	// Description is the enum description.
	Description string

	// Values maps value names to their definitions.
	Values map[string]*ParsedEnumValue

	// Directives applied to this enum.
	Directives []*ParsedDirective
}

// ParsedEnumValue represents an enum value.
type ParsedEnumValue struct {
	// Name is the value name.
	Name string

	// Description is the value description.
	Description string

	// DeprecationReason if the value is deprecated.
	DeprecationReason string

	// Directives applied to this value.
	Directives []*ParsedDirective
}

// ParsedInput represents an input object type.
type ParsedInput struct {
	// Name is the input type name.
	Name string

	// Description is the input description.
	Description string

	// Fields maps field names to their definitions.
	Fields map[string]*ParsedArg

	// Directives applied to this input.
	Directives []*ParsedDirective
}

// ParsedInterface represents an interface type.
type ParsedInterface struct {
	// Name is the interface name.
	Name string

	// Description is the interface description.
	Description string

	// Fields maps field names to their definitions.
	Fields map[string]*ParsedField

	// Directives applied to this interface.
	Directives []*ParsedDirective
}

// ParsedUnion represents a union type.
type ParsedUnion struct {
	// Name is the union name.
	Name string

	// Description is the union description.
	Description string

	// Types lists the member type names.
	Types []string

	// Directives applied to this union.
	Directives []*ParsedDirective
}

// ParsedDirective represents a directive application (e.g., @key(fields: "id")).
type ParsedDirective struct {
	// Name is the directive name (without @).
	Name string

	// Args maps argument names to their values.
	Args map[string]interface{}
}

// ParsedDirectiveDef represents a directive definition.
type ParsedDirectiveDef struct {
	// Name is the directive name.
	Name string

	// Description is the directive description.
	Description string

	// Args maps argument names to their definitions.
	Args map[string]*ParsedArg

	// Locations where this directive can be applied.
	Locations []string

	// IsRepeatable indicates if the directive can be applied multiple times.
	IsRepeatable bool
}

// ParseSDLComplete parses a complete GraphQL SDL using the graphql-go AST parser.
func ParseSDLComplete(sdl string) (*ParsedSchema, error) {
	src := source.NewSource(&source.Source{
		Body: []byte(sdl),
		Name: "schema",
	})

	doc, err := parser.Parse(parser.ParseParams{Source: src})
	if err != nil {
		return nil, fmt.Errorf("failed to parse SDL: %w", err)
	}

	schema := &ParsedSchema{
		Types:      make(map[string]*ParsedType),
		Enums:      make(map[string]*ParsedEnum),
		Inputs:     make(map[string]*ParsedInput),
		Interfaces: make(map[string]*ParsedInterface),
		Unions:     make(map[string]*ParsedUnion),
		Scalars:    []string{},
		Directives: make(map[string]*ParsedDirectiveDef),
	}

	// Process all definitions
	for _, def := range doc.Definitions {
		switch d := def.(type) {
		case *ast.ObjectDefinition:
			parsedType := parseObjectDefinition(d, false)
			switch d.Name.Value {
			case "Query":
				schema.Query = parsedType
			case "Mutation":
				schema.Mutation = parsedType
			case "Subscription":
				schema.Subscription = parsedType
			default:
				schema.Types[d.Name.Value] = parsedType
			}

		case *ast.TypeExtensionDefinition:
			parsedType := parseObjectDefinition(d.Definition, true)
			switch d.Definition.Name.Value {
			case "Query":
				if schema.Query == nil {
					schema.Query = parsedType
				} else {
					mergeType(schema.Query, parsedType)
				}
			case "Mutation":
				if schema.Mutation == nil {
					schema.Mutation = parsedType
				} else {
					mergeType(schema.Mutation, parsedType)
				}
			default:
				if existing, ok := schema.Types[d.Definition.Name.Value]; ok {
					mergeType(existing, parsedType)
				} else {
					schema.Types[d.Definition.Name.Value] = parsedType
				}
			}

		case *ast.EnumDefinition:
			schema.Enums[d.Name.Value] = parseEnumDefinition(d)

		case *ast.InputObjectDefinition:
			schema.Inputs[d.Name.Value] = parseInputDefinition(d)

		case *ast.InterfaceDefinition:
			schema.Interfaces[d.Name.Value] = parseInterfaceDefinition(d)

		case *ast.UnionDefinition:
			schema.Unions[d.Name.Value] = parseUnionDefinition(d)

		case *ast.ScalarDefinition:
			schema.Scalars = append(schema.Scalars, d.Name.Value)

		case *ast.DirectiveDefinition:
			schema.Directives[d.Name.Value] = parseDirectiveDefinition(d)

		case *ast.SchemaDefinition:
			// Schema definition - extract operation types if needed
			for _, op := range d.OperationTypes {
				switch op.Operation {
				case "query":
					if schema.Query == nil {
						schema.Query = &ParsedType{
							Name:   op.Type.Name.Value,
							Fields: make(map[string]*ParsedField),
						}
					}
				case "mutation":
					if schema.Mutation == nil {
						schema.Mutation = &ParsedType{
							Name:   op.Type.Name.Value,
							Fields: make(map[string]*ParsedField),
						}
					}
				case "subscription":
					if schema.Subscription == nil {
						schema.Subscription = &ParsedType{
							Name:   op.Type.Name.Value,
							Fields: make(map[string]*ParsedField),
						}
					}
				}
			}
		}
	}

	return schema, nil
}

// parseObjectDefinition parses an object type definition.
func parseObjectDefinition(def *ast.ObjectDefinition, isExtension bool) *ParsedType {
	t := &ParsedType{
		Name:        def.Name.Value,
		Description: getDescription(def.Description),
		Fields:      make(map[string]*ParsedField),
		Implements:  []string{},
		Directives:  parseDirectives(def.Directives),
		IsExtension: isExtension,
	}

	// Parse interfaces
	for _, iface := range def.Interfaces {
		t.Implements = append(t.Implements, iface.Name.Value)
	}

	// Parse fields
	for _, field := range def.Fields {
		t.Fields[field.Name.Value] = parseFieldDefinition(field)
	}

	return t
}

// parseFieldDefinition parses a field definition.
func parseFieldDefinition(field *ast.FieldDefinition) *ParsedField {
	f := &ParsedField{
		Name:        field.Name.Value,
		Description: getDescription(field.Description),
		Type:        parseTypeRef(field.Type),
		Args:        make(map[string]*ParsedArg),
		Directives:  parseDirectives(field.Directives),
	}

	// Check for deprecation
	for _, dir := range f.Directives {
		if dir.Name == "deprecated" {
			if reason, ok := dir.Args["reason"].(string); ok {
				f.DeprecationReason = reason
			} else {
				f.DeprecationReason = "No longer supported"
			}
		}
	}

	// Parse arguments
	for _, arg := range field.Arguments {
		f.Args[arg.Name.Value] = parseInputValueDefinition(arg)
	}

	return f
}

// parseInputValueDefinition parses an argument or input field definition.
func parseInputValueDefinition(arg *ast.InputValueDefinition) *ParsedArg {
	a := &ParsedArg{
		Name:        arg.Name.Value,
		Description: getDescription(arg.Description),
		Type:        parseTypeRef(arg.Type),
		Directives:  parseDirectives(arg.Directives),
	}

	// Parse default value
	if arg.DefaultValue != nil {
		a.DefaultValue = parseValue(arg.DefaultValue)
	}

	return a
}

// parseTypeRef parses a type reference from the AST.
// Handles types like: String, String!, [String], [String!], [String]!, [String!]!
func parseTypeRef(t ast.Type) *ParsedTypeRef {
	ref := &ParsedTypeRef{}

	// Handle outer NonNull if present (for [Type]!)
	if nonNull, ok := t.(*ast.NonNull); ok {
		innerType := nonNull.Type
		if _, isList := innerType.(*ast.List); isList {
			ref.ListNonNull = true
			t = innerType
		} else {
			ref.NonNull = true
			t = innerType
		}
	}

	// Handle List
	if list, ok := t.(*ast.List); ok {
		ref.IsList = true
		t = list.Type

		// Handle element NonNull (for [Type!])
		if elemNonNull, ok := t.(*ast.NonNull); ok {
			ref.ElementNonNull = true
			t = elemNonNull.Type
		}
	}

	// Extract the base type name
	if named, ok := t.(*ast.Named); ok {
		ref.Name = named.Name.Value
	}

	return ref
}

// parseEnumDefinition parses an enum type definition.
func parseEnumDefinition(def *ast.EnumDefinition) *ParsedEnum {
	e := &ParsedEnum{
		Name:        def.Name.Value,
		Description: getDescription(def.Description),
		Values:      make(map[string]*ParsedEnumValue),
		Directives:  parseDirectives(def.Directives),
	}

	for _, val := range def.Values {
		ev := &ParsedEnumValue{
			Name:        val.Name.Value,
			Description: getDescription(val.Description),
			Directives:  parseDirectives(val.Directives),
		}

		// Check for deprecation
		for _, dir := range ev.Directives {
			if dir.Name == "deprecated" {
				if reason, ok := dir.Args["reason"].(string); ok {
					ev.DeprecationReason = reason
				} else {
					ev.DeprecationReason = "No longer supported"
				}
			}
		}

		e.Values[val.Name.Value] = ev
	}

	return e
}

// parseInputDefinition parses an input object type definition.
func parseInputDefinition(def *ast.InputObjectDefinition) *ParsedInput {
	i := &ParsedInput{
		Name:        def.Name.Value,
		Description: getDescription(def.Description),
		Fields:      make(map[string]*ParsedArg),
		Directives:  parseDirectives(def.Directives),
	}

	for _, field := range def.Fields {
		i.Fields[field.Name.Value] = parseInputValueDefinition(field)
	}

	return i
}

// parseInterfaceDefinition parses an interface type definition.
func parseInterfaceDefinition(def *ast.InterfaceDefinition) *ParsedInterface {
	i := &ParsedInterface{
		Name:        def.Name.Value,
		Description: getDescription(def.Description),
		Fields:      make(map[string]*ParsedField),
		Directives:  parseDirectives(def.Directives),
	}

	for _, field := range def.Fields {
		i.Fields[field.Name.Value] = parseFieldDefinition(field)
	}

	return i
}

// parseUnionDefinition parses a union type definition.
func parseUnionDefinition(def *ast.UnionDefinition) *ParsedUnion {
	u := &ParsedUnion{
		Name:        def.Name.Value,
		Description: getDescription(def.Description),
		Types:       []string{},
		Directives:  parseDirectives(def.Directives),
	}

	for _, t := range def.Types {
		u.Types = append(u.Types, t.Name.Value)
	}

	return u
}

// parseDirectiveDefinition parses a directive definition.
func parseDirectiveDefinition(def *ast.DirectiveDefinition) *ParsedDirectiveDef {
	d := &ParsedDirectiveDef{
		Name:        def.Name.Value,
		Description: getDescription(def.Description),
		Args:        make(map[string]*ParsedArg),
		Locations:   []string{},
	}

	for _, arg := range def.Arguments {
		d.Args[arg.Name.Value] = parseInputValueDefinition(arg)
	}

	for _, loc := range def.Locations {
		d.Locations = append(d.Locations, loc.Value)
	}

	return d
}

// parseDirectives parses directive applications.
func parseDirectives(dirs []*ast.Directive) []*ParsedDirective {
	result := make([]*ParsedDirective, 0, len(dirs))

	for _, dir := range dirs {
		d := &ParsedDirective{
			Name: dir.Name.Value,
			Args: make(map[string]interface{}),
		}

		for _, arg := range dir.Arguments {
			d.Args[arg.Name.Value] = parseValue(arg.Value)
		}

		result = append(result, d)
	}

	return result
}

// parseValue parses a value from the AST.
func parseValue(val ast.Value) interface{} {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case *ast.StringValue:
		return v.Value
	case *ast.IntValue:
		return v.Value
	case *ast.FloatValue:
		return v.Value
	case *ast.BooleanValue:
		return v.Value
	case *ast.EnumValue:
		return v.Value
	case *ast.ListValue:
		result := make([]interface{}, len(v.Values))
		for i, item := range v.Values {
			result[i] = parseValue(item)
		}
		return result
	case *ast.ObjectValue:
		result := make(map[string]interface{})
		for _, field := range v.Fields {
			result[field.Name.Value] = parseValue(field.Value)
		}
		return result
	default:
		return nil
	}
}

// getDescription extracts description text from a StringValue.
func getDescription(desc *ast.StringValue) string {
	if desc == nil {
		return ""
	}
	return desc.Value
}

// mergeType merges fields from src into dst.
func mergeType(dst, src *ParsedType) {
	for name, field := range src.Fields {
		dst.Fields[name] = field
	}
	for _, iface := range src.Implements {
		dst.Implements = append(dst.Implements, iface)
	}
	dst.Directives = append(dst.Directives, src.Directives...)
}

// GetFieldByPath returns a field from the schema by path (e.g., "Query.users").
func (s *ParsedSchema) GetFieldByPath(path string) *ParsedField {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return nil
	}

	typeName, fieldName := parts[0], parts[1]
	var targetType *ParsedType

	switch typeName {
	case "Query":
		targetType = s.Query
	case "Mutation":
		targetType = s.Mutation
	case "Subscription":
		targetType = s.Subscription
	default:
		targetType = s.Types[typeName]
	}

	if targetType == nil {
		return nil
	}

	return targetType.Fields[fieldName]
}

// HasField checks if a field exists in the schema.
func (s *ParsedSchema) HasField(path string) bool {
	return s.GetFieldByPath(path) != nil
}

// GetAllQueryFields returns all Query fields.
func (s *ParsedSchema) GetAllQueryFields() map[string]*ParsedField {
	if s.Query == nil {
		return nil
	}
	return s.Query.Fields
}

// GetAllMutationFields returns all Mutation fields.
func (s *ParsedSchema) GetAllMutationFields() map[string]*ParsedField {
	if s.Mutation == nil {
		return nil
	}
	return s.Mutation.Fields
}

// TypeRefToString converts a ParsedTypeRef to its SDL representation.
func (r *ParsedTypeRef) String() string {
	var sb strings.Builder

	if r.IsList {
		sb.WriteString("[")
		sb.WriteString(r.Name)
		if r.ElementNonNull {
			sb.WriteString("!")
		}
		sb.WriteString("]")
		if r.ListNonNull {
			sb.WriteString("!")
		}
	} else {
		sb.WriteString(r.Name)
		if r.NonNull {
			sb.WriteString("!")
		}
	}

	return sb.String()
}

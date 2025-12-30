package graphql

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// Federation v2 specification support for GraphQL subgraphs.
// Enables integration with Apollo Federation, Cosmo, and other GraphQL routers.

// FederationConfig holds Federation-specific configuration.
type FederationConfig struct {
	// Enabled enables Federation support.
	Enabled bool

	// Version is the Federation version (1 or 2). Defaults to 2.
	Version int
}

// EntityResolver resolves entity references from other subgraphs.
type EntityResolver func(ctx context.Context, representation map[string]interface{}) (interface{}, error)

// FederationSupport manages Federation v2 features for a GraphQL schema.
type FederationSupport struct {
	mu sync.RWMutex

	// entities maps type names to their entity resolvers.
	entities map[string]*EntityDefinition

	// sdl holds the full SDL including Federation directives.
	sdl string

	// version is the Federation version (1 or 2).
	version int
}

// EntityDefinition defines a federated entity type.
type EntityDefinition struct {
	// TypeName is the GraphQL type name.
	TypeName string

	// Keys are the @key directive fields for this entity.
	Keys []EntityKey

	// Resolver resolves entity references.
	Resolver EntityResolver

	// GraphQLType is the graphql-go type for this entity.
	GraphQLType *graphql.Object
}

// EntityKey represents a @key directive on an entity.
type EntityKey struct {
	// Fields is the field selection for this key (e.g., "id" or "id organization { id }").
	Fields string

	// Resolvable indicates if this subgraph can resolve this entity.
	Resolvable bool
}

// NewFederationSupport creates a new Federation support instance.
func NewFederationSupport(version int) *FederationSupport {
	if version == 0 {
		version = 2
	}
	return &FederationSupport{
		entities: make(map[string]*EntityDefinition),
		version:  version,
	}
}

// RegisterEntity registers an entity type with its resolver.
func (f *FederationSupport) RegisterEntity(typeName string, keys []EntityKey, resolver EntityResolver, gqlType *graphql.Object) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.entities[typeName] = &EntityDefinition{
		TypeName:    typeName,
		Keys:        keys,
		Resolver:    resolver,
		GraphQLType: gqlType,
	}
}

// SetSDL sets the schema SDL for the _service field.
func (f *FederationSupport) SetSDL(sdl string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sdl = sdl
}

// GetSDL returns the schema SDL.
func (f *FederationSupport) GetSDL() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.sdl
}

// AnyScalar is the _Any scalar type for entity representations.
var AnyScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "_Any",
	Description: "The _Any scalar is used to pass representations of entities from external services into the _entities field for federation.",
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

// FieldSetScalar is the FieldSet scalar type used in Federation directives.
var FieldSetScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "FieldSet",
	Description: "A set of fields for Federation directives.",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		return value
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		if v, ok := valueAST.(*ast.StringValue); ok {
			return v.Value
		}
		return nil
	},
})

// LinkPurpose enum for @link directive.
var LinkPurposeEnum = graphql.NewEnum(graphql.EnumConfig{
	Name:        "link__Purpose",
	Description: "Purpose of the link import.",
	Values: graphql.EnumValueConfigMap{
		"SECURITY": &graphql.EnumValueConfig{
			Value:       "SECURITY",
			Description: "Security-related imports.",
		},
		"EXECUTION": &graphql.EnumValueConfig{
			Value:       "EXECUTION",
			Description: "Execution-related imports.",
		},
	},
})

// ServiceType is the _Service type for Federation.
var ServiceType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "_Service",
	Description: "The _Service type represents the federated service and its schema.",
	Fields: graphql.Fields{
		"sdl": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The schema of this service in SDL format.",
		},
	},
})

// CreateServiceField creates the _service Query field.
func (f *FederationSupport) CreateServiceField() *graphql.Field {
	return &graphql.Field{
		Type:        graphql.NewNonNull(ServiceType),
		Description: "The _service field returns the schema SDL for this federated service.",
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			return map[string]interface{}{
				"sdl": f.GetSDL(),
			}, nil
		},
	}
}

// CreateEntitiesField creates the _entities Query field.
func (f *FederationSupport) CreateEntitiesField() *graphql.Field {
	// Create the _Entity union type dynamically based on registered entities
	entityUnion := f.createEntityUnion()

	return &graphql.Field{
		Type:        graphql.NewList(entityUnion),
		Description: "The _entities field resolves entity references from other subgraphs.",
		Args: graphql.FieldConfigArgument{
			"representations": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(AnyScalar))),
				Description: "Entity representations to resolve.",
			},
		},
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			representations, ok := p.Args["representations"].([]interface{})
			if !ok {
				return nil, fmt.Errorf("representations must be an array")
			}

			results := make([]interface{}, len(representations))

			for i, rep := range representations {
				repMap, ok := rep.(map[string]interface{})
				if !ok {
					results[i] = nil
					continue
				}

				typeName, ok := repMap["__typename"].(string)
				if !ok {
					results[i] = nil
					continue
				}

				f.mu.RLock()
				entity, exists := f.entities[typeName]
				f.mu.RUnlock()

				if !exists || entity.Resolver == nil {
					results[i] = nil
					continue
				}

				result, err := entity.Resolver(p.Context, repMap)
				if err != nil {
					results[i] = nil
					continue
				}

				results[i] = result
			}

			return results, nil
		},
	}
}

// createEntityUnion creates the _Entity union type from registered entities.
func (f *FederationSupport) createEntityUnion() *graphql.Union {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]*graphql.Object, 0, len(f.entities))
	for _, entity := range f.entities {
		if entity.GraphQLType != nil {
			types = append(types, entity.GraphQLType)
		}
	}

	// If no entities registered, create a placeholder type
	if len(types) == 0 {
		placeholderType := graphql.NewObject(graphql.ObjectConfig{
			Name: "_EntityPlaceholder",
			Fields: graphql.Fields{
				"_placeholder": &graphql.Field{
					Type: graphql.String,
				},
			},
		})
		types = append(types, placeholderType)
	}

	return graphql.NewUnion(graphql.UnionConfig{
		Name:        "_Entity",
		Description: "Union of all entity types that can be resolved by this service.",
		Types:       types,
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			if valueMap, ok := p.Value.(map[string]interface{}); ok {
				if typeName, ok := valueMap["__typename"].(string); ok {
					f.mu.RLock()
					entity, exists := f.entities[typeName]
					f.mu.RUnlock()
					if exists && entity.GraphQLType != nil {
						return entity.GraphQLType
					}
				}
			}
			return nil
		},
	})
}

// GetFederationDirectives returns the Federation v2 directive definitions as SDL.
func GetFederationDirectives(version int) string {
	if version < 2 {
		// Federation v1 directives
		return `
directive @key(fields: String!) repeatable on OBJECT | INTERFACE
directive @external on FIELD_DEFINITION
directive @requires(fields: String!) on FIELD_DEFINITION
directive @provides(fields: String!) on FIELD_DEFINITION
directive @extends on OBJECT | INTERFACE
`
	}

	// Federation v2 directives
	return `
extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.0", import: ["@key", "@shareable", "@provides", "@external", "@tag", "@extends", "@override", "@inaccessible", "@requires"])

directive @key(fields: FieldSet!, resolvable: Boolean = true) repeatable on OBJECT | INTERFACE
directive @shareable on OBJECT | FIELD_DEFINITION
directive @provides(fields: FieldSet!) on FIELD_DEFINITION
directive @external(reason: String) on OBJECT | FIELD_DEFINITION
directive @tag(name: String!) repeatable on FIELD_DEFINITION | OBJECT | INTERFACE | UNION | ARGUMENT_DEFINITION | SCALAR | ENUM | ENUM_VALUE | INPUT_OBJECT | INPUT_FIELD_DEFINITION
directive @extends on OBJECT | INTERFACE
directive @override(from: String!) on FIELD_DEFINITION
directive @inaccessible on FIELD_DEFINITION | OBJECT | INTERFACE | UNION | ARGUMENT_DEFINITION | SCALAR | ENUM | ENUM_VALUE | INPUT_OBJECT | INPUT_FIELD_DEFINITION
directive @requires(fields: FieldSet!) on FIELD_DEFINITION

scalar FieldSet
scalar _Any
`
}

// ParseFederationDirectives extracts Federation directives from SDL.
func ParseFederationDirectives(sdl string) map[string][]EntityKey {
	entities := make(map[string][]EntityKey)

	lines := strings.Split(sdl, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for type definition with @key directives
		if strings.HasPrefix(line, "type ") && strings.Contains(line, "@key") {
			// Extract type name
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				typeName := parts[1]
				// Remove any trailing characters like { or @
				for _, suffix := range []string{"{", "@"} {
					if idx := strings.Index(typeName, suffix); idx > 0 {
						typeName = typeName[:idx]
					}
				}
				typeName = strings.TrimSpace(typeName)

				// Extract all @key directives from this line
				keys := parseAllKeyDirectives(line)
				if len(keys) > 0 {
					entities[typeName] = keys
				}
			}
		}
	}

	return entities
}

// parseAllKeyDirectives extracts all @key directives from a line.
func parseAllKeyDirectives(line string) []EntityKey {
	var keys []EntityKey

	// Find all occurrences of @key
	remaining := line
	for {
		keyIdx := strings.Index(remaining, "@key")
		if keyIdx == -1 {
			break
		}

		// Extract the directive
		key := parseKeyDirective(remaining[keyIdx:])
		if key != nil {
			keys = append(keys, *key)
		}

		// Move past this @key to find the next one
		remaining = remaining[keyIdx+4:]
	}

	return keys
}

// parseKeyDirective extracts the fields from a @key directive.
func parseKeyDirective(line string) *EntityKey {
	// Find @key(fields: "...")
	keyIdx := strings.Index(line, "@key")
	if keyIdx == -1 {
		return nil
	}

	// Find fields parameter
	fieldsStart := strings.Index(line[keyIdx:], "fields:")
	if fieldsStart == -1 {
		fieldsStart = strings.Index(line[keyIdx:], "fields :")
	}
	if fieldsStart == -1 {
		return nil
	}

	fieldsStart += keyIdx

	// Find the opening quote
	quoteStart := strings.Index(line[fieldsStart:], "\"")
	if quoteStart == -1 {
		return nil
	}
	quoteStart += fieldsStart

	// Find the closing quote
	quoteEnd := strings.Index(line[quoteStart+1:], "\"")
	if quoteEnd == -1 {
		return nil
	}
	quoteEnd += quoteStart + 1

	fields := line[quoteStart+1 : quoteEnd]

	// Check for resolvable parameter
	resolvable := true
	if strings.Contains(line, "resolvable: false") || strings.Contains(line, "resolvable:false") {
		resolvable = false
	}

	return &EntityKey{
		Fields:     fields,
		Resolvable: resolvable,
	}
}

// GenerateFederatedSDL generates the complete SDL including Federation directives.
func (f *FederationSupport) GenerateFederatedSDL(schemaSDL string) string {
	var sb strings.Builder

	// Add Federation directive definitions
	sb.WriteString(GetFederationDirectives(f.version))
	sb.WriteString("\n")

	// Add the schema SDL
	sb.WriteString(schemaSDL)

	return sb.String()
}

// HasEntities returns true if any entities are registered.
func (f *FederationSupport) HasEntities() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.entities) > 0
}

// GetEntityNames returns the names of all registered entities.
func (f *FederationSupport) GetEntityNames() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	names := make([]string, 0, len(f.entities))
	for name := range f.entities {
		names = append(names, name)
	}
	return names
}

package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/matutetandil/mycel/internal/validate"
)

// TestSDLParser tests the SDL parser with various schemas.
func TestSDLParser(t *testing.T) {
	t.Run("parse basic types", func(t *testing.T) {
		sdl := `
type User {
	id: ID!
	name: String!
	email: String
	age: Int
	active: Boolean!
}

type Query {
	users: [User!]!
	user(id: ID!): User
}

type Mutation {
	createUser(name: String!, email: String!): User!
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		// Check User type
		user, ok := parsed.Types["User"]
		if !ok {
			t.Fatal("User type not found")
		}

		if len(user.Fields) != 5 {
			t.Errorf("expected 5 fields, got %d", len(user.Fields))
		}

		// Check id field
		idField := user.Fields["id"]
		if idField == nil {
			t.Fatal("id field not found")
		}
		if idField.Type.Name != "ID" || !idField.Type.NonNull {
			t.Errorf("expected ID!, got %s (nonNull=%v)", idField.Type.Name, idField.Type.NonNull)
		}

		// Check Query type
		if parsed.Query == nil {
			t.Fatal("Query type not found")
		}
		if len(parsed.Query.Fields) != 2 {
			t.Errorf("expected 2 Query fields, got %d", len(parsed.Query.Fields))
		}

		// Check users field - should be [User!]!
		usersField := parsed.Query.Fields["users"]
		if usersField == nil {
			t.Fatal("users field not found")
		}
		if !usersField.Type.IsList || !usersField.Type.ListNonNull || !usersField.Type.ElementNonNull {
			t.Errorf("expected [User!]!, got %s", usersField.Type.String())
		}

		// Check user field has argument
		userField := parsed.Query.Fields["user"]
		if userField == nil {
			t.Fatal("user field not found")
		}
		if len(userField.Args) != 1 {
			t.Errorf("expected 1 argument, got %d", len(userField.Args))
		}

		// Check Mutation type
		if parsed.Mutation == nil {
			t.Fatal("Mutation type not found")
		}
	})

	t.Run("parse enums", func(t *testing.T) {
		sdl := `
enum Status {
	ACTIVE
	INACTIVE
	PENDING
}

type User {
	status: Status!
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		status, ok := parsed.Enums["Status"]
		if !ok {
			t.Fatal("Status enum not found")
		}

		if len(status.Values) != 3 {
			t.Errorf("expected 3 enum values, got %d", len(status.Values))
		}
	})

	t.Run("parse interfaces", func(t *testing.T) {
		sdl := `
interface Node {
	id: ID!
}

type User implements Node {
	id: ID!
	name: String!
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		node, ok := parsed.Interfaces["Node"]
		if !ok {
			t.Fatal("Node interface not found")
		}

		if len(node.Fields) != 1 {
			t.Errorf("expected 1 field, got %d", len(node.Fields))
		}

		user := parsed.Types["User"]
		if user == nil {
			t.Fatal("User type not found")
		}

		if len(user.Implements) != 1 || user.Implements[0] != "Node" {
			t.Errorf("expected User to implement Node")
		}
	})

	t.Run("parse input types", func(t *testing.T) {
		sdl := `
input CreateUserInput {
	name: String!
	email: String!
	age: Int
}

type Mutation {
	createUser(input: CreateUserInput!): User!
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		input, ok := parsed.Inputs["CreateUserInput"]
		if !ok {
			t.Fatal("CreateUserInput not found")
		}

		if len(input.Fields) != 3 {
			t.Errorf("expected 3 fields, got %d", len(input.Fields))
		}
	})

	t.Run("parse directives", func(t *testing.T) {
		sdl := `
type User @key(fields: "id") {
	id: ID!
	name: String! @deprecated(reason: "Use fullName")
	fullName: String!
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		user := parsed.Types["User"]
		if user == nil {
			t.Fatal("User type not found")
		}

		// Check @key directive on type
		if len(user.Directives) != 1 {
			t.Errorf("expected 1 directive, got %d", len(user.Directives))
		} else if user.Directives[0].Name != "key" {
			t.Errorf("expected @key directive, got @%s", user.Directives[0].Name)
		}

		// Check @deprecated directive on field
		nameField := user.Fields["name"]
		if nameField == nil {
			t.Fatal("name field not found")
		}
		if nameField.DeprecationReason == "" {
			t.Error("expected deprecation reason")
		}
	})
}

// TestSDLConverter tests SDL to graphql-go conversion.
func TestSDLConverter(t *testing.T) {
	t.Run("convert basic types", func(t *testing.T) {
		sdl := `
type User {
	id: ID!
	name: String!
	email: String
}

type Query {
	users: [User!]!
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		converter := NewSDLConverter()
		if err := converter.Convert(parsed); err != nil {
			t.Fatalf("failed to convert SDL: %v", err)
		}

		// Check User type was created
		userType := converter.GetType("User")
		if userType == nil {
			t.Fatal("User type not created")
		}

		// Check fields
		fields := userType.Fields()
		if len(fields) != 3 {
			t.Errorf("expected 3 fields, got %d", len(fields))
		}

		// Check Query fields
		queryFields := converter.GetQueryFields()
		if queryFields == nil {
			t.Fatal("Query fields not created")
		}

		usersField := queryFields["users"]
		if usersField == nil {
			t.Fatal("users field not created")
		}
	})

	t.Run("convert enums", func(t *testing.T) {
		sdl := `
enum Status {
	ACTIVE
	INACTIVE
}
`
		parsed, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		converter := NewSDLConverter()
		if err := converter.Convert(parsed); err != nil {
			t.Fatalf("failed to convert SDL: %v", err)
		}

		statusEnum := converter.GetEnum("Status")
		if statusEnum == nil {
			t.Fatal("Status enum not created")
		}

		values := statusEnum.Values()
		if len(values) != 2 {
			t.Errorf("expected 2 values, got %d", len(values))
		}
	})
}

// TestSchemaBuilderModes tests the schema builder with SDL and different modes.
func TestSchemaBuilderModes(t *testing.T) {
	t.Run("schema-first mode", func(t *testing.T) {
		sdl := `
type User {
	id: ID!
	name: String!
}

type Query {
	users: [User!]!
	user(id: ID!): User
}

type Mutation {
	createUser(name: String!): User!
}
`
		builder := NewSchemaBuilder()
		err := builder.ParseSDL(sdl)
		if err != nil {
			t.Fatalf("failed to parse SDL: %v", err)
		}

		// Check mode was set
		if builder.GetMode() != SchemaModeSDL {
			t.Errorf("expected SDL mode, got %s", builder.GetMode())
		}

		// Check fields exist
		if !builder.HasField("Query.users") {
			t.Error("users field not found")
		}
		if !builder.HasField("Query.user") {
			t.Error("user field not found")
		}
		if !builder.HasField("Mutation.createUser") {
			t.Error("createUser field not found")
		}

		// Register handlers
		handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return []map[string]interface{}{
				{"id": "1", "name": "Test User"},
			}, nil
		}

		if err := builder.RegisterHandler("Query.users", handler); err != nil {
			t.Fatalf("failed to register handler: %v", err)
		}

		// Build schema
		schema, err := builder.Build()
		if err != nil {
			t.Fatalf("failed to build schema: %v", err)
		}

		// Execute query
		result := graphql.Do(graphql.Params{
			Schema:        *schema,
			RequestString: `{ users { id name } }`,
		})

		if len(result.Errors) > 0 {
			t.Errorf("query failed: %v", result.Errors)
		}

		if result.Data == nil {
			t.Error("no data returned")
		}
	})

	t.Run("dynamic mode", func(t *testing.T) {
		builder := NewSchemaBuilder()

		// Register handler without SDL
		handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return []map[string]interface{}{
				{"id": 1, "name": "Dynamic User"},
			}, nil
		}

		if err := builder.RegisterHandler("Query.users", handler); err != nil {
			t.Fatalf("failed to register handler: %v", err)
		}

		// Check mode
		if builder.GetMode() != SchemaModeDynamic {
			t.Errorf("expected Dynamic mode, got %s", builder.GetMode())
		}

		// Build and test
		schema, err := builder.Build()
		if err != nil {
			t.Fatalf("failed to build schema: %v", err)
		}

		result := graphql.Do(graphql.Params{
			Schema:        *schema,
			RequestString: `{ users }`,
		})

		if len(result.Errors) > 0 {
			t.Errorf("query failed: %v", result.Errors)
		}
	})

	t.Run("register handler with return type", func(t *testing.T) {
		builder := NewSchemaBuilder()

		// Create a custom type
		userType := graphql.NewObject(graphql.ObjectConfig{
			Name: "User",
			Fields: graphql.Fields{
				"id":   &graphql.Field{Type: graphql.ID},
				"name": &graphql.Field{Type: graphql.String},
			},
		})
		builder.RegisterType("User", userType)

		handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return []map[string]interface{}{
				{"id": "1", "name": "Typed User"},
			}, nil
		}

		// Register with return type
		if err := builder.RegisterHandlerWithReturnType("Query.users", handler, "User[]"); err != nil {
			t.Fatalf("failed to register handler: %v", err)
		}

		schema, err := builder.Build()
		if err != nil {
			t.Fatalf("failed to build schema: %v", err)
		}

		// The field should have the correct type
		queryType := schema.QueryType()
		usersField := queryType.Fields()["users"]
		if usersField == nil {
			t.Fatal("users field not found")
		}

		// Check it's a list type
		_, isList := usersField.Type.(*graphql.List)
		_, isNonNullList := usersField.Type.(*graphql.NonNull)
		if !isList && !isNonNullList {
			t.Errorf("expected list type, got %T", usersField.Type)
		}
	})
}

// TestHCLConverter tests HCL TypeSchema to GraphQL conversion.
func TestHCLConverter(t *testing.T) {
	t.Run("convert basic type", func(t *testing.T) {
		schema := &validate.TypeSchema{
			Name: "User",
			Fields: []validate.FieldSchema{
				{Name: "id", Type: "string", Required: true},
				{Name: "name", Type: "string", Required: true},
				{Name: "email", Type: "string", Required: false},
				{Name: "age", Type: "number", Required: false},
				{Name: "active", Type: "boolean", Required: true},
			},
		}

		converter := NewHCLConverter()
		gqlType, err := converter.ConvertTypeSchema(schema)
		if err != nil {
			t.Fatalf("failed to convert type: %v", err)
		}

		fields := gqlType.Fields()
		if len(fields) != 5 {
			t.Errorf("expected 5 fields, got %d", len(fields))
		}

		// Check field types
		idField := fields["id"]
		if idField == nil {
			t.Fatal("id field not found")
		}

		// Check input type was created
		inputType := converter.GetInput("UserInput")
		if inputType == nil {
			t.Error("UserInput not created")
		}
	})

	t.Run("convert with enum constraint", func(t *testing.T) {
		schema := &validate.TypeSchema{
			Name: "Order",
			Fields: []validate.FieldSchema{
				{
					Name:     "status",
					Type:     "string",
					Required: true,
					Constraints: []validate.Constraint{
						&validate.EnumConstraint{Values: []string{"pending", "completed", "cancelled"}},
					},
				},
			},
		}

		converter := NewHCLConverter()
		_, err := converter.ConvertTypeSchema(schema)
		if err != nil {
			t.Fatalf("failed to convert type: %v", err)
		}

		// Check enum was created
		statusEnum := converter.GetEnum("StatusEnum")
		if statusEnum == nil {
			t.Error("StatusEnum not created")
		} else {
			values := statusEnum.Values()
			if len(values) != 3 {
				t.Errorf("expected 3 enum values, got %d", len(values))
			}
		}
	})

	t.Run("convert with format constraints", func(t *testing.T) {
		schema := &validate.TypeSchema{
			Name: "Contact",
			Fields: []validate.FieldSchema{
				{
					Name:     "email",
					Type:     "string",
					Required: true,
					Constraints: []validate.Constraint{
						&validate.FormatConstraint{Format: "email"},
					},
				},
			},
		}

		converter := NewHCLConverter()
		gqlType, err := converter.ConvertTypeSchema(schema)
		if err != nil {
			t.Fatalf("failed to convert type: %v", err)
		}

		emailField := gqlType.Fields()["email"]
		if emailField == nil {
			t.Fatal("email field not found")
		}

		// The type should be Email scalar (wrapped in NonNull)
		nonNull, ok := emailField.Type.(*graphql.NonNull)
		if !ok {
			t.Fatal("expected NonNull wrapper")
		}

		scalar, ok := nonNull.OfType.(*graphql.Scalar)
		if !ok {
			t.Fatalf("expected Scalar type, got %T", nonNull.OfType)
		}

		if scalar.Name() != "Email" {
			t.Errorf("expected Email scalar, got %s", scalar.Name())
		}
	})

	t.Run("generate SDL from HCL", func(t *testing.T) {
		schema := &validate.TypeSchema{
			Name: "Product",
			Fields: []validate.FieldSchema{
				{Name: "id", Type: "id", Required: true},
				{Name: "name", Type: "string", Required: true},
				{Name: "price", Type: "number", Required: true},
			},
		}

		converter := NewHCLConverter()
		converter.ConvertTypeSchema(schema)

		sdl := converter.GenerateSDL()

		// Check SDL contains expected definitions
		if !strings.Contains(sdl, "type Product") {
			t.Error("SDL should contain type Product")
		}
		if !strings.Contains(sdl, "input ProductInput") {
			t.Error("SDL should contain input ProductInput")
		}
	})
}

// TestSDLGenerator tests SDL generation.
func TestSDLGenerator(t *testing.T) {
	t.Run("generate from type schemas", func(t *testing.T) {
		schemas := map[string]*validate.TypeSchema{
			"User": {
				Name: "User",
				Fields: []validate.FieldSchema{
					{Name: "id", Type: "id", Required: true},
					{Name: "name", Type: "string", Required: true},
					{Name: "email", Type: "string", Required: false},
				},
			},
			"Product": {
				Name: "Product",
				Fields: []validate.FieldSchema{
					{Name: "id", Type: "id", Required: true},
					{Name: "name", Type: "string", Required: true},
					{Name: "price", Type: "number", Required: true},
				},
			},
		}

		generator := NewSDLGenerator()
		sdl := generator.GenerateFromTypeSchemas(schemas)

		// Check SDL contains both types
		if !strings.Contains(sdl, "type User") {
			t.Error("SDL should contain type User")
		}
		if !strings.Contains(sdl, "type Product") {
			t.Error("SDL should contain type Product")
		}
		if !strings.Contains(sdl, "input UserInput") {
			t.Error("SDL should contain input UserInput")
		}
		if !strings.Contains(sdl, "scalar JSON") {
			t.Error("SDL should contain scalar JSON")
		}
	})

	t.Run("generate with query fields", func(t *testing.T) {
		generator := NewSDLGenerator()

		generator.SetQueryFields(graphql.Fields{
			"users": &graphql.Field{
				Type: graphql.NewList(JSONScalar),
				Args: graphql.FieldConfigArgument{
					"limit": &graphql.ArgumentConfig{Type: graphql.Int},
				},
			},
		})

		sdl := generator.Generate()

		if !strings.Contains(sdl, "type Query") {
			t.Error("SDL should contain type Query")
		}
		if !strings.Contains(sdl, "users") {
			t.Error("SDL should contain users field")
		}
	})
}

// TestResolveReturnType tests return type parsing.
func TestResolveReturnType(t *testing.T) {
	builder := NewSchemaBuilder()

	// Register a custom type
	userType := graphql.NewObject(graphql.ObjectConfig{
		Name: "User",
		Fields: graphql.Fields{
			"id":   &graphql.Field{Type: graphql.ID},
			"name": &graphql.Field{Type: graphql.String},
		},
	})
	builder.RegisterType("User", userType)

	tests := []struct {
		input    string
		expected string
	}{
		{"String", "String"},
		{"String!", "String!"},
		{"[String]", "[String]"},
		{"[String!]!", "[String!]!"},
		{"User[]", "[User!]!"},
		{"User", "User"},
		{"User!", "User!"},
		{"Int", "Int"},
		{"Float", "Float"},
		{"Boolean", "Boolean"},
		{"ID", "ID"},
		{"JSON", "JSON"},
		{"DateTime", "DateTime"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := builder.resolveReturnType(tc.input)
			resultStr := formatGraphQLType(result)
			if resultStr != tc.expected {
				t.Errorf("for %s: expected %s, got %s", tc.input, tc.expected, resultStr)
			}
		})
	}
}

// TestCustomScalars tests custom scalar types.
func TestCustomScalars(t *testing.T) {
	t.Run("JSON scalar", func(t *testing.T) {
		// Test serialization
		input := map[string]interface{}{"key": "value"}
		result := JSONScalar.Serialize(input)
		if result == nil {
			t.Error("JSON scalar should serialize maps")
		}

		// Test parsing
		parsed := JSONScalar.ParseValue(input)
		if parsed == nil {
			t.Error("JSON scalar should parse maps")
		}
	})

	t.Run("DateTime scalar", func(t *testing.T) {
		// Test serialization from string
		result := DateTimeScalar.Serialize("2024-01-15T10:30:00Z")
		if result != "2024-01-15T10:30:00Z" {
			t.Errorf("expected date string, got %v", result)
		}

		// Test parsing
		parsed := DateTimeScalar.ParseValue("2024-01-15T10:30:00Z")
		if parsed == nil {
			t.Error("DateTime should parse ISO strings")
		}
	})

	t.Run("Date scalar", func(t *testing.T) {
		result := DateScalar.Serialize("2024-01-15")
		if result != "2024-01-15" {
			t.Errorf("expected date string, got %v", result)
		}
	})

	t.Run("Email scalar validation", func(t *testing.T) {
		// Valid email
		result := EmailScalar.ParseValue("test@example.com")
		if result == nil {
			t.Error("should accept valid email")
		}

		// Invalid email
		result = EmailScalar.ParseValue("invalid")
		if result != nil {
			t.Error("should reject invalid email")
		}
	})

	t.Run("UUID scalar validation", func(t *testing.T) {
		// Valid UUID
		result := UUIDScalar.ParseValue("550e8400-e29b-41d4-a716-446655440000")
		if result == nil {
			t.Error("should accept valid UUID")
		}

		// Invalid UUID
		result = UUIDScalar.ParseValue("not-a-uuid")
		if result != nil {
			t.Error("should reject invalid UUID")
		}
	})
}

// TestTypeRefString tests ParsedTypeRef.String().
func TestTypeRefString(t *testing.T) {
	tests := []struct {
		ref      ParsedTypeRef
		expected string
	}{
		{
			ref:      ParsedTypeRef{Name: "String"},
			expected: "String",
		},
		{
			ref:      ParsedTypeRef{Name: "String", NonNull: true},
			expected: "String!",
		},
		{
			ref:      ParsedTypeRef{Name: "String", IsList: true},
			expected: "[String]",
		},
		{
			ref:      ParsedTypeRef{Name: "String", IsList: true, ListNonNull: true},
			expected: "[String]!",
		},
		{
			ref:      ParsedTypeRef{Name: "String", IsList: true, ElementNonNull: true},
			expected: "[String!]",
		},
		{
			ref:      ParsedTypeRef{Name: "String", IsList: true, ElementNonNull: true, ListNonNull: true},
			expected: "[String!]!",
		},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := tc.ref.String()
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}

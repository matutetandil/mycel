package graphql

import (
	"testing"
)

// A schema written as SDL, read back.
//
// This is the path a service takes when it is given a .graphql file rather than
// HCL: the text is parsed into a description and that description is turned
// into a runnable schema. Everything about the API a caller sees comes through
// here — whether a field is a list, whether it may be null, whether an argument
// is required — and a mistake is silent: the field is simply not what was
// written.

func parsed(t *testing.T, sdl string) *ParsedSchema {
	t.Helper()
	schema, err := ParseSDLComplete(sdl)
	if err != nil {
		t.Fatalf("parsing the schema: %v", err)
	}
	return schema
}

func fieldNamed(t *testing.T, typ *ParsedType, name string) *ParsedField {
	t.Helper()
	for _, f := range typ.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("type %s has no field %s", typ.Name, name)
	return nil
}

func TestWhatAFieldIsDeclaredToBe(t *testing.T) {
	schema := parsed(t, `
type Query {
  users: [User!]!
  user(id: ID!, includeDeleted: Boolean = false): User
}

type User {
  id: ID!
  name: String
  tags: [String]
  posts: [Post!]
}

type Post {
  id: ID!
}
`)

	query := schema.Query
	if query == nil {
		t.Fatal("the schema has no Query type")
	}

	// [User!]! — a list that is there, of users that are there.
	users := fieldNamed(t, query, "users").Type
	if users.Name != "User" || !users.IsList || !users.ListNonNull || !users.ElementNonNull {
		t.Errorf("users is declared [User!]! and came back %+v", users)
	}

	// User — nullable, single.
	user := fieldNamed(t, query, "user").Type
	if user.Name != "User" || user.IsList || user.NonNull {
		t.Errorf("user is declared User and came back %+v", user)
	}

	// [String] — a nullable list of nullable strings.
	tags := fieldNamed(t, schema.Types["User"], "tags").Type
	if tags.Name != "String" || !tags.IsList || tags.ListNonNull || tags.ElementNonNull {
		t.Errorf("tags is declared [String] and came back %+v", tags)
	}

	// [Post!] — a nullable list whose elements are there.
	posts := fieldNamed(t, schema.Types["User"], "posts").Type
	if !posts.IsList || posts.ListNonNull || !posts.ElementNonNull {
		t.Errorf("posts is declared [Post!] and came back %+v", posts)
	}
}

func TestWhatAnArgumentIsDeclaredToBe(t *testing.T) {
	schema := parsed(t, `
type Query {
  user(id: ID!, includeDeleted: Boolean = false, first: Int): User
}
type User { id: ID! }
`)

	args := fieldNamed(t, schema.Query, "user").Args
	byName := map[string]*ParsedArg{}
	for _, a := range args {
		byName[a.Name] = a
	}

	if len(byName) != 3 {
		t.Fatalf("the field declares 3 arguments and %d came back", len(byName))
	}
	if id := byName["id"]; id == nil || !id.Type.NonNull {
		t.Errorf("id is declared ID! and came back %+v", id)
	}
	if flag := byName["includeDeleted"]; flag == nil || flag.DefaultValue == nil {
		t.Errorf("includeDeleted is declared with a default and came back %+v", flag)
	}
	if first := byName["first"]; first == nil || first.Type.NonNull {
		t.Errorf("first is declared Int and came back %+v", first)
	}
}

func TestTheOtherKindsOfDefinition(t *testing.T) {
	schema := parsed(t, `
enum Status { ACTIVE INACTIVE }

interface Node { id: ID! }

input NewUser {
  name: String!
  email: String
}

union SearchResult = User | Post

type User implements Node { id: ID! }
type Post implements Node { id: ID! }

type Query { search(q: String!): [SearchResult!]! }
`)

	if status := schema.Enums["Status"]; status == nil || len(status.Values) != 2 {
		t.Errorf("the enum came back as %+v", status)
	}
	if node := schema.Interfaces["Node"]; node == nil || len(node.Fields) != 1 {
		t.Errorf("the interface came back as %+v", node)
	}
	if input := schema.Inputs["NewUser"]; input == nil || len(input.Fields) != 2 {
		t.Errorf("the input came back as %+v", input)
	}
	if union := schema.Unions["SearchResult"]; union == nil || len(union.Types) != 2 {
		t.Errorf("the union came back as %+v", union)
	}
	// The types that say they implement it have to remember they do.
	if user := schema.Types["User"]; user == nil || len(user.Implements) != 1 {
		t.Errorf("User implements Node and came back with %v", user.Implements)
	}
}

func TestAnSDLSchemaBecomesARunnableOne(t *testing.T) {
	// The description is only worth something if it converts.
	schema := parsed(t, `
type Query {
  users: [User!]!
  user(id: ID!): User
}

type User {
  id: ID!
  name: String
  status: Status
}

enum Status { ACTIVE INACTIVE }
`)

	converter := NewSDLConverter()
	if err := converter.Convert(schema); err != nil {
		t.Fatalf("converting: %v", err)
	}

	built := converter.GetType("User")
	if built == nil {
		t.Fatal("User did not survive the conversion")
	}
	fields := built.Fields()
	for _, want := range []string{"id", "name", "status"} {
		if _, found := fields[want]; !found {
			t.Errorf("User lost its %s field in the conversion", want)
		}
	}
	if converter.GetEnum("Status") == nil {
		t.Error("the enum did not survive the conversion")
	}
}

func TestWhichTypeIsTheRootWhenItIsRenamed(t *testing.T) {
	// A schema block may call the roots something else, and everything
	// afterwards depends on knowing which type answers a query.
	schema := parsed(t, `
schema {
  query: RootQuery
  mutation: RootMutation
}

type RootQuery { hello: String }
type RootMutation { greet(name: String!): String }
`)

	if schema.Query == nil || schema.Query.Name != "RootQuery" {
		t.Errorf("the query root came back as %+v", schema.Query)
	}
	if schema.Mutation == nil || schema.Mutation.Name != "RootMutation" {
		t.Errorf("the mutation root came back as %+v", schema.Mutation)
	}
}

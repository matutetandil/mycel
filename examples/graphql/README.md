# GraphQL Example

This example demonstrates the GraphQL connector with two approaches for schema generation:

1. **Schema-first**: Define types in SDL file (`.graphql`), Mycel auto-connects flows
2. **HCL-first**: Define types in HCL, Mycel generates GraphQL schema automatically

Both approaches produce the same result: a fully typed GraphQL API.

## Directory Structure

```
graphql/
├── config.hcl          # Service configuration
├── connectors.hcl      # GraphQL server + SQLite database
├── flows.hcl           # GraphQL operations (Query/Mutation)
├── schema.graphql      # SDL schema (for schema-first approach)
└── README.md           # This file
```

## Quick Start

```bash
# From project root
cd examples/graphql

# Create data directory
mkdir -p data

# Run the service
mycel start --config .

# Access GraphQL Playground
open http://localhost:4000/playground
```

## Approach 1: Schema-first

Define your types in a `.graphql` SDL file, and Mycel automatically:
- Parses the SDL with full AST
- Creates typed GraphQL fields
- Connects flows to the schema fields
- Converts database snake_case to GraphQL camelCase

### Schema Definition (schema.graphql)

```graphql
type User {
  id: Int!
  email: String!
  name: String!
  createdAt: String   # Automatically mapped from created_at
}

input UserInput {
  email: String!
  name: String!
}

type Query {
  users: [User!]!
  user(id: Int!): User
}

type Mutation {
  createUser(input: UserInput!): User!
  updateUser(id: Int!, input: UpdateUserInput!): User
  deleteUser(id: Int!): Boolean!
}
```

### Connector Configuration

```hcl
connector "graphql_api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  schema {
    path = "./schema.graphql"  # SDL defines the types
  }
}
```

### Flow Configuration

```hcl
# The flow connects to the schema field automatically
flow "get_users" {
  from {
    connector = "graphql_api"
    operation = "Query.users"   # Matches Query.users in SDL
  }

  to {
    connector = "database"
    target    = "users"
  }
}
```

## Approach 2: HCL-first

Define your types in HCL files, and Mycel automatically generates the GraphQL schema. Use the `returns` attribute to specify the return type.

### Type Definition (types/user.hcl)

```hcl
type "User" {
  id         = id
  email      = string { format = "email" }
  name       = string { min_length = 1 }
  externalId = string
  createdAt  = string
}

type "UserInput" {
  email = string { format = "email" }
  name  = string { min_length = 1 }
}

type "MutationResult" {
  id       = id
  affected = number
}
```

### Flow with Return Type

```hcl
flow "get_users" {
  from {
    connector = "graphql_api"
    operation = "Query.users"
  }

  to {
    connector = "database"
    target    = "users"
  }

  returns = "User[]"   # Return type: array of User
}

flow "get_user" {
  from {
    connector = "graphql_api"
    operation = "Query.user"
  }

  to {
    connector = "database"
    target    = "users"
  }

  returns = "User"     # Return type: single User (auto-unwrap)
}

flow "create_user" {
  from {
    connector = "graphql_api"
    operation = "Mutation.createUser"
  }

  transform {
    email      = "lower(input.email)"
    name       = "input.name"
    created_at = "now()"
  }

  to {
    connector = "database"
    target    = "users"
  }

  returns = "User"     # Returns the created user
}
```

### Type Mapping: HCL to GraphQL

| HCL Type | GraphQL Type |
|----------|--------------|
| `id` | `ID` |
| `string` | `String` |
| `number` | `Int` or `Float` |
| `boolean` | `Boolean` |
| `array` | `[Type]` |
| `object` | Custom type |

## Features

### Column Mapping (snake_case to camelCase)

Database columns are automatically converted:
- `external_id` -> `externalId`
- `created_at` -> `createdAt`
- `updated_at` -> `updatedAt`

### Smart Resolver

For non-list return types, single-element arrays are automatically unwrapped:

```graphql
# Query.user returns User (not [User])
# If database returns [{id: 1, name: "John"}], GraphQL returns {id: 1, name: "John"}
```

### GraphQL Variables

Full support for parameterized queries:

```graphql
query GetUser($id: ID!) {
  user(id: $id) {
    id
    email
    name
  }
}
```

### Playground UI

Access the GraphQL Playground at `/playground` for:
- Interactive query builder
- Schema documentation
- Query history
- Variable editor

## Testing

```bash
# Query all users
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ users { id email name } }"}'

# Query single user
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ user(id: 1) { id email name } }"}'

# Create user
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { createUser(input: { email: \"test@example.com\", name: \"Test\" }) { id email } }"}'

# With variables
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation CreateUser($input: UserInput!) { createUser(input: $input) { id email } }",
    "variables": { "input": { "email": "test@example.com", "name": "Test" } }
  }'
```

## GraphQL Client

Connect to external GraphQL APIs:

```hcl
connector "external_api" {
  type     = "graphql"
  driver   = "client"
  endpoint = "https://api.example.com/graphql"

  auth {
    type  = "bearer"
    token = env("API_TOKEN")
  }

  # Or OAuth2
  # auth {
  #   type          = "oauth2"
  #   client_id     = env("CLIENT_ID")
  #   client_secret = env("CLIENT_SECRET")
  #   token_url     = "https://auth.example.com/oauth/token"
  # }

  timeout     = "30s"
  retry_count = 3
  retry_delay = "1s"
}
```

Use as enrichment source:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  enrich "pricing" {
    connector = "external_api"
    operation = "query { product(id: $id) { price currency } }"
    params {
      id = "input.id"
    }
  }

  transform {
    id       = "input.id"
    price    = "enriched.pricing.product.price"
    currency = "enriched.pricing.product.currency"
  }

  to {
    connector = "database"
    target    = "products"
  }
}
```

## Custom Scalars

Supported scalar types:
- `ID` - Unique identifier
- `String` - UTF-8 string
- `Int` - 32-bit integer
- `Float` - Double-precision float
- `Boolean` - true/false
- `JSON` - Arbitrary JSON data
- `DateTime` - ISO 8601 date/time
- `Date` - ISO 8601 date
- `Time` - ISO 8601 time

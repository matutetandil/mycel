# Named Operations Example

This example demonstrates **named operations** for connectors, a feature that improves encapsulation and enables better tooling support (like mycel-studio introspection).

## What are Named Operations?

Operations are defined in the connector configuration and referenced by name in flows:

```hcl
# Connector defines operations
connector "api" {
  type = "rest"
  port = 8080

  operation "list_users" {
    method = "GET"
    path   = "/users"
  }
}

# Flow references by name
flow "get_users" {
  from {
    connector = "api"
    operation = "list_users"
  }
  ...
}
```

## Benefits

1. **Encapsulation**: Connector operations are defined where they belong - in the connector
2. **Reusability**: Same operation can be used by multiple flows
3. **Documentation**: Operations can have descriptions and parameter docs
4. **Validation**: Parameters can have types, required flags, and defaults
5. **Tooling**: mycel-studio can introspect available operations

## File Structure

```
named-operations/
├── connectors.hcl  # Connector definitions with named operations
├── flows.hcl       # Flows referencing operations by name
└── README.md       # This file
```

## Connector Operations

### REST Connector

```hcl
connector "api" {
  type = "rest"
  port = 8080

  operation "list_users" {
    method      = "GET"
    path        = "/users"
    description = "List all users with pagination"

    param "limit" {
      type        = "number"
      default     = 100
      description = "Maximum number of users to return"
    }
  }
}
```

### Database Connector

```hcl
connector "db" {
  type   = "database"
  driver = "sqlite"
  database = ":memory:"

  operation "user_by_id" {
    query       = "SELECT * FROM users WHERE id = :id"
    description = "Get a user by ID"

    param "id" {
      type     = "string"
      required = true
    }
  }
}
```

## Parameter Definitions

Parameters support the following attributes:

| Attribute     | Type     | Description                                    |
|---------------|----------|------------------------------------------------|
| `type`        | string   | string, number, boolean, array, object         |
| `required`    | bool     | Whether the parameter is mandatory             |
| `default`     | any      | Default value if not provided                  |
| `description` | string   | Documentation for the parameter                |
| `in`          | string   | Where the param comes from (path, query, etc.) |
| `min`         | number   | Minimum value (numbers)                        |
| `max`         | number   | Maximum value (numbers)                        |
| `min_length`  | number   | Minimum length (strings)                       |
| `max_length`  | number   | Maximum length (strings)                       |
| `pattern`     | string   | Regex pattern (strings)                        |
| `enum`        | []string | Allowed values                                 |

## Running the Example

```bash
# Validate the configuration
mycel validate --config=./examples/named-operations

# Start the service (requires database setup)
mycel start --config=./examples/named-operations
```

## Supported Connector Types

Named operations work with all connector types:

| Connector | Operation Attributes                    |
|-----------|----------------------------------------|
| REST      | method, path                           |
| Database  | query, table                           |
| GraphQL   | operation_type, field                  |
| gRPC      | service, rpc                           |
| MQ        | exchange, routing_key, queue           |
| TCP       | protocol, action                       |
| File      | path_pattern                           |
| S3        | path_pattern                           |
| Cache     | key_pattern, ttl                       |
| Exec      | command, args                          |

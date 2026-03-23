# GraphQL Query: Get all users
# GraphQL: { users { id email name created_at } }

flow "get_users" {
  from {
    connector = "graphql_api"
    operation = "Query.users"
  }

  to {
    connector = "database"
    target    = "users"
  }
}

# GraphQL Query: Get single user by ID
# GraphQL: { user(id: 1) { id email name created_at } }

flow "get_user" {
  from {
    connector = "graphql_api"
    operation = "Query.user"
  }

  to {
    connector = "database"
    target    = "users"
    query     = "SELECT * FROM users WHERE id = :id"
  }
}

# GraphQL Mutation: Create a new user
# GraphQL: mutation { createUser(input: { email: "...", name: "..." }) { id email name } }

flow "create_user" {
  from {
    connector = "graphql_api"
    operation = "Mutation.createUser"
  }

  transform {
    email      = "input.input.email"
    name       = "input.input.name"
    created_at = "now()"
  }

  to {
    connector = "database"
    target    = "users"
  }
}

# GraphQL Mutation: Update a user
# GraphQL: mutation { updateUser(id: 1, input: { name: "..." }) { id email name } }

flow "update_user" {
  from {
    connector = "graphql_api"
    operation = "Mutation.updateUser"
  }

  transform {
    id   = "input.id"
    name = "input.input.name"
  }

  to {
    connector = "database"
    target    = "users"
  }
}

# GraphQL Mutation: Delete a user
# GraphQL: mutation { deleteUser(id: 1) }

flow "delete_user" {
  from {
    connector = "graphql_api"
    operation = "Mutation.deleteUser"
  }

  to {
    connector = "database"
    target    = "users"
    query     = "DELETE FROM users WHERE id = :id"
  }
}

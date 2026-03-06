# GraphQL flows

flow "gql_list_users" {
  from {
    connector = "graphql_api"
    operation = "Query.users"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
  returns = "user[]"
}

flow "gql_get_user" {
  from {
    connector = "graphql_api"
    operation = "Query.user"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
  returns = "user"
}

flow "gql_create_user" {
  from {
    connector = "graphql_api"
    operation = "Mutation.createUser"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
  returns = "user"
}

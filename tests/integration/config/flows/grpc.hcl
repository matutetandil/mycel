# gRPC flows

flow "grpc_get_user" {
  from {
    connector = "grpc_api"
    operation = "UserService/GetUser"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}

flow "grpc_list_users" {
  from {
    connector = "grpc_api"
    operation = "UserService/ListUsers"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}

flow "grpc_create_user" {
  from {
    connector = "grpc_api"
    operation = "UserService/CreateUser"
  }
  to {
    connector = "postgres"
    target    = "users"
    operation = "INSERT"
  }
}

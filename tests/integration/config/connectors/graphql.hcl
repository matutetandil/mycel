# GraphQL server
connector "graphql_api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
    headers = ["Content-Type", "Authorization"]
  }
}

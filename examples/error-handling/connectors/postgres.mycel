# PostgreSQL connector with circuit breaker protection (via aspect)

connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = env("DB_PORT", "5432")
  user     = env("DB_USER", "mycel")
  password = env("DB_PASSWORD", "secret")
  database = env("DB_NAME", "mycel_app")
}

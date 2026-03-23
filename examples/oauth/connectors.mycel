connector "api" {
  type = "rest"
  port = 3000
}

connector "google_auth" {
  type   = "oauth"
  driver = "google"

  client_id     = env("GOOGLE_CLIENT_ID")
  client_secret = env("GOOGLE_CLIENT_SECRET")
  redirect_uri  = "http://localhost:3000/auth/google/callback"
  scopes        = ["openid", "email", "profile"]
}

connector "github_auth" {
  type   = "oauth"
  driver = "github"

  client_id     = env("GITHUB_CLIENT_ID")
  client_secret = env("GITHUB_CLIENT_SECRET")
  redirect_uri  = "http://localhost:3000/auth/github/callback"
  scopes        = ["read:user", "user:email"]
}

connector "db" {
  type   = "database"
  driver = "sqlite"
  dsn    = "users.db"
}

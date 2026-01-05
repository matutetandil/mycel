# Authentication Service Example
# This example demonstrates a complete authentication microservice using Mycel

service {
  name    = "auth-service"
  version = "1.0.0"
}

# Database for user storage
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = 5432
  database = env("DB_NAME", "auth")
  user     = env("DB_USER", "postgres")
  password = env("DB_PASSWORD", "postgres")
}

# REST API connector (exposes auth endpoints)
connector "api" {
  type = "rest"
  port = 8080

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    headers = ["Authorization", "Content-Type"]
  }
}

# Authentication configuration
auth {
  # Use the standard preset (balanced security)
  preset = "standard"

  # Token/session storage - using in-memory for simplicity
  # In production, use Redis:
  # storage {
  #   driver  = "redis"
  #   address = env("REDIS_URL")
  # }

  # User storage
  users {
    connector = connector.postgres
    table     = "users"

    fields {
      id            = "id"
      email         = "email"
      password_hash = "password_hash"
      created_at    = "created_at"
      updated_at    = "updated_at"
    }
  }

  # JWT configuration
  jwt {
    secret           = env("JWT_SECRET", "change-this-in-production")
    access_lifetime  = "1h"
    refresh_lifetime = "7d"
    issuer           = "auth-service"
    rotation         = true  # Rotate refresh tokens on use
  }

  # Password policy
  password {
    min_length      = 8
    require_upper   = true
    require_lower   = true
    require_number  = true
    require_special = false
    history         = 3  # Can't reuse last 3 passwords
  }

  # MFA configuration (optional for users)
  mfa {
    required = "optional"
    methods  = ["totp"]

    totp {
      issuer = "Auth Service"
      digits = 6
      period = 30
    }
  }

  # Security features
  security {
    brute_force {
      enabled      = true
      max_attempts = 5
      window       = "15m"
      lockout_time = "15m"
      track_by     = "ip+user"
    }

    replay_protection {
      enabled = true
      window  = "5m"
    }
  }

  # Session management
  sessions {
    max_active       = 5
    idle_timeout     = "1h"
    absolute_timeout = "24h"
    allow_list       = true
    allow_revoke     = true
    on_max_reached   = "revoke_oldest"
  }

  # Customize endpoint paths (optional)
  endpoints {
    prefix = "/auth"

    login    { path = "/login",    method = "POST", enabled = true }
    logout   { path = "/logout",   method = "POST", enabled = true }
    register { path = "/register", method = "POST", enabled = true }
    refresh  { path = "/refresh",  method = "POST", enabled = true }
    me       { path = "/me",       method = "GET",  enabled = true }

    sessions_list   { path = "/sessions",     method = "GET",    enabled = true }
    sessions_revoke { path = "/sessions/:id", method = "DELETE", enabled = true }

    password_change { path = "/change-password", method = "POST", enabled = true }
  }

  # Audit logging
  audit {
    enabled   = true
    connector = connector.postgres
    table     = "auth_audit_log"
    events    = ["login", "logout", "failed_login", "register", "password_change", "sso_login"]
  }

  # SSO / Social Login (Phase 5.1d)
  sso {
    # Account linking configuration
    linking {
      enabled              = true
      match_by             = "email"     # "email", "none"
      require_verification = true
      on_match             = "link"      # "link", "prompt", "reject"
    }
  }

  # Social Login Providers
  social {
    google {
      client_id     = env("GOOGLE_CLIENT_ID")
      client_secret = env("GOOGLE_CLIENT_SECRET")
      scopes        = ["openid", "email", "profile"]
    }

    github {
      client_id     = env("GITHUB_CLIENT_ID")
      client_secret = env("GITHUB_CLIENT_SECRET")
      scopes        = ["read:user", "user:email"]
    }

    # Apple Sign In (requires additional setup)
    # apple {
    #   client_id   = env("APPLE_CLIENT_ID")
    #   team_id     = env("APPLE_TEAM_ID")
    #   key_id      = env("APPLE_KEY_ID")
    #   private_key = env("APPLE_PRIVATE_KEY")
    # }
  }

  # Enterprise OIDC Providers
  # oidc "okta" {
  #   issuer        = env("OKTA_ISSUER")
  #   client_id     = env("OKTA_CLIENT_ID")
  #   client_secret = env("OKTA_CLIENT_SECRET")
  #   scopes        = ["openid", "email", "profile"]
  # }

  # oidc "azure" {
  #   issuer        = "https://login.microsoftonline.com/${TENANT_ID}/v2.0"
  #   client_id     = env("AZURE_CLIENT_ID")
  #   client_secret = env("AZURE_CLIENT_SECRET")
  #   scopes        = ["openid", "email", "profile"]
  # }
}

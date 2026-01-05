service {
  name = "notifications"
  port = 8080
}

# REST API for sending notifications
connector "api" {
  type = "rest"
  port = 8080
}

# Email via SMTP
connector "email_smtp" {
  type     = "email"
  driver   = "smtp"
  host     = env("SMTP_HOST", "smtp.gmail.com")
  port     = env("SMTP_PORT", "587")
  username = env("SMTP_USER", "")
  password = env("SMTP_PASS", "")
  from     = env("SMTP_FROM", "notifications@example.com")
  tls      = "starttls"
}

# Email via SendGrid (alternative)
# connector "email_sendgrid" {
#   type    = "email"
#   driver  = "sendgrid"
#   api_key = env("SENDGRID_API_KEY", "")
#   from    = env("SENDGRID_FROM", "notifications@example.com")
# }

# Slack
connector "slack" {
  type        = "slack"
  webhook_url = env("SLACK_WEBHOOK_URL", "")
  channel     = "#notifications"
  username    = "Mycel Bot"
  icon_emoji  = ":robot_face:"
}

# Discord
connector "discord" {
  type        = "discord"
  webhook_url = env("DISCORD_WEBHOOK_URL", "")
  username    = "Mycel Bot"
}

# SMS via Twilio
connector "sms" {
  type        = "sms"
  driver      = "twilio"
  account_sid = env("TWILIO_ACCOUNT_SID", "")
  auth_token  = env("TWILIO_AUTH_TOKEN", "")
  from        = env("TWILIO_FROM", "")
}

# Push notifications via FCM
connector "push" {
  type       = "push"
  driver     = "fcm"
  server_key = env("FCM_SERVER_KEY", "")
}

# Inbound webhooks
connector "webhooks_in" {
  type   = "webhook"
  driver = "inbound"
  path   = "/webhooks/events"

  signature {
    header    = "X-Signature-256"
    algorithm = "hmac-sha256"
    secret    = env("WEBHOOK_SECRET", "")
  }
}

# Outbound webhooks
connector "webhooks_out" {
  type   = "webhook"
  driver = "outbound"
  url    = env("WEBHOOK_TARGET_URL", "https://example.com/webhook")

  signature {
    header    = "X-Signature-256"
    algorithm = "hmac-sha256"
    secret    = env("WEBHOOK_SECRET", "")
  }

  retry {
    max_attempts = 3
    initial_delay = "1s"
    max_delay = "30s"
  }
}

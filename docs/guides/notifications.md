# Notifications

Mycel includes native connectors for sending notifications through multiple channels. Each connector is configured once and used as a flow target like any other connector.

## Overview

| Connector | Channel | Config |
|-----------|---------|--------|
| `email` | SMTP email | `host`, `port`, `username`, `password` |
| `slack` | Slack messages | `token` (Bot token) |
| `discord` | Discord messages | `token` (Bot token) |
| `sms` | SMS via Twilio | `account_sid`, `auth_token`, `from` |
| `push` | FCM / APNs | `provider`, `credentials_file` |
| `webhook` | HTTP callbacks | `url`, `method`, `headers` |

For complete per-connector reference, see [docs/connectors/notifications.md](../connectors/notifications.md).

## Email

```hcl
connector "mailer" {
  type     = "email"
  host     = env("SMTP_HOST")
  port     = 587
  username = env("SMTP_USER")
  password = env("SMTP_PASSWORD")
  from     = "noreply@example.com"
  tls      = true
}

flow "send_welcome_email" {
  from {
    connector = "rabbit"
    operation = "user.registered"
  }

  transform {
    to      = "input.email"
    subject = "'Welcome to MyApp, ' + input.name + '!'"
    body    = "'Hello ' + input.name + ', your account is ready.'"
  }

  to {
    connector = "mailer"
    operation = "send"
  }
}
```

## Slack

```hcl
connector "slack_alerts" {
  type  = "slack"
  token = env("SLACK_BOT_TOKEN")
}

flow "alert_on_error" {
  # This would typically be an aspect
  from {
    connector = "api"
    operation = "POST /critical"
  }

  transform {
    channel = "'#alerts'"
    text    = "'Critical event: ' + input.event_type"
  }

  to {
    connector = "slack_alerts"
    operation = "chat.postMessage"
  }
}
```

## Discord

```hcl
connector "discord_bot" {
  type  = "discord"
  token = env("DISCORD_BOT_TOKEN")
}

flow "send_discord_notification" {
  from {
    connector = "rabbit"
    operation = "events"
  }

  transform {
    channel_id = "'123456789'"
    content    = "input.message"
  }

  to {
    connector = "discord_bot"
    operation = "channels.messages"
  }
}
```

## SMS (Twilio)

```hcl
connector "sms_service" {
  type        = "sms"
  account_sid = env("TWILIO_ACCOUNT_SID")
  auth_token  = env("TWILIO_AUTH_TOKEN")
  from        = env("TWILIO_FROM_NUMBER")
}

flow "send_otp_sms" {
  from {
    connector = "api"
    operation = "POST /auth/otp"
  }

  transform {
    to   = "input.phone"
    body = "'Your OTP: ' + input.code"
  }

  to {
    connector = "sms_service"
    operation = "send"
  }
}
```

## Push Notifications

```hcl
# FCM (Firebase Cloud Messaging)
connector "push_fcm" {
  type             = "push"
  provider         = "fcm"
  credentials_file = "./firebase-credentials.json"
}

flow "send_push" {
  from {
    connector = "rabbit"
    operation = "push.notifications"
  }

  transform {
    token = "input.device_token"
    title = "input.title"
    body  = "input.message"
    data  = "input.extra_data"
  }

  to {
    connector = "push_fcm"
    operation = "send"
  }
}
```

## Webhook

```hcl
connector "external_webhook" {
  type   = "webhook"
  url    = env("WEBHOOK_URL")
  method = "POST"
  headers = {
    "X-Webhook-Secret" = env("WEBHOOK_SECRET")
    "Content-Type"     = "application/json"
  }
}

flow "notify_external_system" {
  from {
    connector = "rabbit"
    operation = "events"
  }
  to {
    connector = "external_webhook"
    operation = "send"
  }
}
```

## Using with Aspects

The most powerful use of notifications is with aspects — send alerts or notifications across multiple flows without duplicating code:

```hcl
aspect "notify_on_error" {
  when = "on_error"
  on   = ["api_*"]

  action {
    connector = "slack_alerts"
    operation = "chat.postMessage"
    transform {
      channel = "'#errors'"
      text    = "'Flow failed: ' + _flow + ' | Error: ' + error"
    }
  }
}
```

This sends a Slack message whenever any flow in `flows/api/` fails.

## Configurable API URLs

All notification connectors support overriding the default API URL for testing or custom endpoints:

```hcl
connector "slack_test" {
  type    = "slack"
  token   = env("SLACK_TOKEN")
  api_url = "http://localhost:8080"  # Point to a mock for testing
}
```

## See Also

- [Connector Reference: Notifications](../connectors/notifications.md) — complete field reference
- [Concepts: Aspects](../guides/extending.md#aspects) — applying notifications as cross-cutting concerns

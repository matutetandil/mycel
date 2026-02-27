# Notifications

Send notifications across multiple channels: email, Slack, Discord, SMS, push notifications, and webhooks. Each channel is a separate connector type.

## Email (SMTP)

```hcl
connector "email" {
  type     = "email"
  driver   = "smtp"
  host     = env("SMTP_HOST")
  port     = 587
  username = env("SMTP_USER")
  password = env("SMTP_PASS")
  from     = "notifications@example.com"
}
```

## Slack

```hcl
connector "slack" {
  type        = "slack"
  webhook_url = env("SLACK_WEBHOOK_URL")
  channel     = "#notifications"
  username    = "Mycel Bot"
}
```

## Discord

```hcl
connector "discord" {
  type        = "discord"
  webhook_url = env("DISCORD_WEBHOOK_URL")
  username    = "Mycel Bot"
}
```

## SMS (Twilio)

```hcl
connector "sms" {
  type        = "sms"
  driver      = "twilio"
  account_sid = env("TWILIO_ACCOUNT_SID")
  auth_token  = env("TWILIO_AUTH_TOKEN")
  from        = env("TWILIO_FROM")
}
```

## Push (FCM / APNs)

```hcl
connector "push" {
  type       = "push"
  driver     = "fcm"
  server_key = env("FCM_SERVER_KEY")
}
```

## Webhook

```hcl
# Inbound — receive external webhooks
connector "webhooks_in" {
  type   = "webhook"
  driver = "inbound"
  path   = "/webhooks/events"
}

# Outbound — send webhooks to external systems
connector "webhooks_out" {
  type   = "webhook"
  driver = "outbound"
  url    = env("WEBHOOK_TARGET_URL")
}
```

## Operations

All notification connectors support a `send` operation as target. Webhook inbound supports `receive` as source.

| Connector | Operation | Direction | Description |
|-----------|-----------|-----------|-------------|
| `email` | `send` | target | Send an email |
| `slack` | `send` | target | Post a Slack message |
| `discord` | `send` | target | Post a Discord message |
| `sms` | `send` | target | Send an SMS |
| `push` | `send` | target | Send a push notification |
| `webhook` (outbound) | `send` | target | Send an HTTP webhook |
| `webhook` (inbound) | `receive` | source | Receive an external webhook |

## Example

```hcl
flow "send_welcome_email" {
  from { connector = "api", operation = "POST /notify/email" }
  transform {
    to       = "[{'email': input.to, 'name': input.name ?? ''}]"
    subject  = "input.subject"
    textBody = "input.body"
  }
  to { connector = "email", operation = "send" }
}

flow "alert_slack" {
  from { connector = "rabbit", operation = "alerts" }
  transform {
    channel = "'#alerts'"
    text    = "'Alert: ' + input.message"
  }
  to { connector = "slack", operation = "send" }
}
```

See the [notifications example](../../examples/notifications/) for a complete working setup.

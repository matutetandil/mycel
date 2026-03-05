# Notifications

Send notifications across multiple channels: email, Slack, Discord, SMS, push notifications, and webhooks. Each channel is a separate connector type with a `send` operation as target.

## Operations

| Connector | Operation | Direction | Description |
|-----------|-----------|-----------|-------------|
| `email` | `send` | target | Send an email |
| `slack` | `send` | target | Post a Slack message |
| `discord` | `send` | target | Post a Discord message |
| `sms` | `send` | target | Send an SMS |
| `push` | `send` | target | Send a push notification |
| `webhook` (outbound) | `send` | target | Send an HTTP webhook |
| `webhook` (inbound) | `receive` | source | Receive an external webhook |

---

## Slack

Two modes: **webhook** (simple, no auth) or **token** (full Slack API with `chat.postMessage`). If both are set, webhook takes precedence.

```hcl
# Webhook mode — simple, no OAuth needed
connector "slack_webhook" {
  type        = "slack"
  webhook_url = env("SLACK_WEBHOOK_URL")
  channel     = "#notifications"
  username    = "Mycel Bot"
  icon_emoji  = ":robot_face:"
}

# Token mode — full API access, requires Bot OAuth token
connector "slack_api" {
  type    = "slack"
  token   = env("SLACK_BOT_TOKEN")
  channel = "#notifications"
}
```

### Connector Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `webhook_url` | string | **yes**\* | — | Slack incoming webhook URL |
| `token` | string | **yes**\* | — | Bot/User OAuth token (for API mode) |
| `channel` | string | optional | — | Default channel to post to |
| `username` | string | optional | — | Display username (webhook mode only) |
| `icon_emoji` | string | optional | — | Display emoji icon (webhook mode only) |
| `icon_url` | string | optional | — | Display icon URL (webhook mode only) |
| `timeout` | duration | optional | `30s` | HTTP request timeout |

\* Either `webhook_url` or `token` is required. Not both.

### Transform Fields

| Field | Type | Description |
|-------|------|-------------|
| `channel` | string | Channel to post to (overrides default) |
| `text` | string | Message text (required) |
| `thread_ts` | string | Thread timestamp (reply to thread) |
| `username` | string | Override display username |
| `icon_emoji` | string | Override display emoji |
| `icon_url` | string | Override display icon URL |
| `unfurl_links` | bool | Unfurl links in message |
| `unfurl_media` | bool | Unfurl media in message |
| `mrkdwn` | bool | Enable markdown parsing |

### Example

```hcl
flow "alert_slack" {
  from { connector = "rabbit", operation = "alerts" }
  transform {
    channel = "'#alerts'"
    text    = "'Alert: ' + input.body.message"
  }
  to { connector = "slack_api", operation = "send" }
}
```

---

## Discord

Two modes: **webhook** (simple) or **bot token** (full Discord API). If both are set, webhook takes precedence.

```hcl
# Webhook mode
connector "discord_webhook" {
  type        = "discord"
  webhook_url = env("DISCORD_WEBHOOK_URL")
  username    = "Mycel Bot"
  avatar_url  = "https://example.com/bot.png"
}

# Bot token mode — requires channel_id
connector "discord_bot" {
  type       = "discord"
  bot_token  = env("DISCORD_BOT_TOKEN")
  channel_id = env("DISCORD_CHANNEL_ID")
}
```

### Connector Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `webhook_url` | string | **yes**\* | — | Discord webhook URL |
| `bot_token` | string | **yes**\* | — | Discord bot token (for API mode) |
| `channel_id` | string | optional | — | Default channel ID (required for bot mode) |
| `username` | string | optional | — | Display username (webhook mode only) |
| `avatar_url` | string | optional | — | Display avatar URL (webhook mode only) |
| `timeout` | duration | optional | `30s` | HTTP request timeout |

\* Either `webhook_url` or `bot_token` is required. Not both.

### Transform Fields

| Field | Type | Description |
|-------|------|-------------|
| `content` | string | Message text (required) |
| `username` | string | Override display username |
| `avatar_url` | string | Override avatar URL |
| `tts` | bool | Text-to-speech |
| `thread_name` | string | Create a thread (forum channels) |

### Example

```hcl
flow "notify_discord" {
  from { connector = "api", operation = "POST /notify/discord" }
  transform {
    content = "'New order: ' + input.body.order_id"
  }
  to { connector = "discord_webhook", operation = "send" }
}
```

---

## Email

Three drivers: **smtp**, **sendgrid**, and **ses** (AWS Simple Email Service).

```hcl
# SMTP
connector "email_smtp" {
  type     = "email"
  driver   = "smtp"
  host     = env("SMTP_HOST")
  port     = 587
  username = env("SMTP_USER")
  password = env("SMTP_PASS")
  from     = "notifications@example.com"
  tls      = "starttls"
}

# SendGrid
connector "email_sendgrid" {
  type    = "email"
  driver  = "sendgrid"
  api_key = env("SENDGRID_API_KEY")
  from    = "notifications@example.com"
}

# AWS SES
connector "email_ses" {
  type              = "email"
  driver            = "ses"
  region            = "us-east-1"
  access_key_id     = env("AWS_ACCESS_KEY_ID")
  secret_access_key = env("AWS_SECRET_ACCESS_KEY")
  from              = "notifications@example.com"
}
```

### Connector Options (all drivers)

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `driver` | string | **yes** | — | `smtp`, `sendgrid`, or `ses` |
| `from` | string | optional | — | Default sender email address |
| `from_name` | string | optional | — | Default sender display name |
| `reply_to` | string | optional | — | Default reply-to address |

### SMTP Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `host` | string | **yes** | — | SMTP server hostname |
| `port` | int | optional | `587` | SMTP port (25, 465, 587) |
| `username` | string | optional | — | SMTP auth username |
| `password` | string | optional | — | SMTP auth password |
| `tls` | string | optional | `starttls` | TLS mode: `none`, `starttls`, `tls` |
| `timeout` | duration | optional | `30s` | Request timeout |
| `pool_size` | int | optional | `5` | Connection pool size |

### SendGrid Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `api_key` | string | **yes** | — | SendGrid API key |
| `endpoint` | string | optional | `https://api.sendgrid.com` | API endpoint |
| `timeout` | duration | optional | `30s` | Request timeout |

### AWS SES Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `region` | string | optional | `us-east-1` | AWS region |
| `access_key_id` | string | optional | — | AWS access key (falls back to default credential chain) |
| `secret_access_key` | string | optional | — | AWS secret key |
| `configuration_set` | string | optional | — | SES configuration set name |
| `timeout` | duration | optional | `30s` | Request timeout |

### Transform Fields

| Field | Type | Description |
|-------|------|-------------|
| `to` | list | Recipients: `[{email, name}]` (required) |
| `cc` | list | CC recipients: `[{email, name}]` |
| `bcc` | list | BCC recipients: `[{email, name}]` |
| `subject` | string | Email subject (required) |
| `textBody` | string | Plain text body |
| `htmlBody` | string | HTML body |
| `from` | string | Override sender address |
| `reply_to` | string | Override reply-to |
| `template_id` | string | Provider template ID (SendGrid/SES) |
| `template_data` | map | Template variables |
| `track_opens` | bool | Enable open tracking |
| `track_clicks` | bool | Enable click tracking |
| `tags` | list | Email tags |

### Example

```hcl
flow "send_welcome_email" {
  from { connector = "api", operation = "POST /notify/email" }
  transform {
    to       = "[{'email': input.body.email, 'name': input.body.name}]"
    subject  = "'Welcome, ' + input.body.name + '!'"
    htmlBody = "'<h1>Welcome!</h1><p>Your account is ready.</p>'"
  }
  to { connector = "email_smtp", operation = "send" }
}
```

---

## SMS

Two drivers: **twilio** and **sns** (AWS Simple Notification Service).

```hcl
# Twilio
connector "sms_twilio" {
  type        = "sms"
  driver      = "twilio"
  account_sid = env("TWILIO_ACCOUNT_SID")
  auth_token  = env("TWILIO_AUTH_TOKEN")
  from        = env("TWILIO_FROM_NUMBER")
}

# AWS SNS
connector "sms_sns" {
  type              = "sms"
  driver            = "sns"
  region            = "us-east-1"
  access_key_id     = env("AWS_ACCESS_KEY_ID")
  secret_access_key = env("AWS_SECRET_ACCESS_KEY")
  sms_type          = "Transactional"
}
```

### Twilio Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `account_sid` | string | **yes** | — | Twilio Account SID |
| `auth_token` | string | **yes** | — | Twilio Auth Token |
| `from` | string | **yes** | — | Sender phone number or messaging service SID |
| `timeout` | duration | optional | `30s` | Request timeout |

### AWS SNS Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `region` | string | optional | `us-east-1` | AWS region |
| `access_key_id` | string | optional | — | AWS access key |
| `secret_access_key` | string | optional | — | AWS secret key |
| `sender_id` | string | optional | — | SMS sender ID |
| `sms_type` | string | optional | — | `Promotional` or `Transactional` |
| `timeout` | duration | optional | `30s` | Request timeout |

### Transform Fields

| Field | Type | Description |
|-------|------|-------------|
| `to` | string | Recipient phone number (required) |
| `body` | string | SMS message text (required) |
| `from` | string | Override sender number |

### Example

```hcl
flow "send_verification_sms" {
  from { connector = "api", operation = "POST /notify/sms" }
  transform {
    to   = "input.body.phone"
    body = "'Your code is: ' + input.body.code"
  }
  to { connector = "sms_twilio", operation = "send" }
}
```

---

## Push Notifications

Two drivers: **fcm** (Firebase Cloud Messaging) and **apns** (Apple Push Notification Service).

```hcl
# Firebase Cloud Messaging
connector "push_fcm" {
  type       = "push"
  driver     = "fcm"
  server_key = env("FCM_SERVER_KEY")
}

# Apple Push Notifications
connector "push_apns" {
  type        = "push"
  driver      = "apns"
  team_id     = env("APNS_TEAM_ID")
  key_id      = env("APNS_KEY_ID")
  private_key = env("APNS_PRIVATE_KEY")
  bundle_id   = "com.example.myapp"
  production  = true
}
```

### FCM Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `server_key` | string | **yes** | — | Legacy FCM server key |
| `project_id` | string | optional | — | Firebase project ID (v1 API) |
| `service_account_json` | string | optional | — | Path to service account credentials |
| `timeout` | duration | optional | `30s` | Request timeout |

### APNs Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `team_id` | string | **yes** | — | Apple Developer Team ID |
| `key_id` | string | **yes** | — | APNs auth key ID |
| `private_key` | string | **yes** | — | APNs P8 private key content |
| `bundle_id` | string | optional | — | App bundle identifier |
| `production` | bool | optional | `false` | Use production endpoint (vs sandbox) |
| `timeout` | duration | optional | `30s` | Request timeout |

### Transform Fields

| Field | Type | Description |
|-------|------|-------------|
| `token` | string | Single device token |
| `tokens` | list | Multiple device tokens |
| `topic` | string | Topic-based messaging (FCM) |
| `title` | string | Notification title |
| `body` | string | Notification body |
| `data` | map | Custom data payload |
| `priority` | string | `high` or `normal` |
| `ttl` | int | Time-to-live in seconds |
| `collapse_key` | string | Collapsible notification key |

### Example

```hcl
flow "send_push" {
  from { connector = "api", operation = "POST /notify/push" }
  transform {
    token = "input.body.device_token"
    title = "'New message'"
    body  = "input.body.message"
    data  = "{'order_id': input.body.order_id}"
  }
  to { connector = "push_fcm", operation = "send" }
}
```

---

## Webhook

Two modes: **outbound** (send webhooks to external systems) and **inbound** (receive external webhooks). Outbound includes HMAC signature verification and automatic retries.

```hcl
# Outbound — send webhooks
connector "webhooks_out" {
  type               = "webhook"
  mode               = "outbound"
  url                = env("WEBHOOK_TARGET_URL")
  method             = "POST"
  secret             = env("WEBHOOK_SECRET")
  signature_header   = "X-Webhook-Signature"
  signature_algorithm = "hmac-sha256"
  timeout            = "10s"

  retry {
    max_attempts = 3
    initial_delay = "1s"
    max_delay     = "30s"
    multiplier    = 2.0
  }
}

# Inbound — receive external webhooks
connector "webhooks_in" {
  type = "webhook"
  mode = "inbound"
  path = "/webhooks/events"
}
```

### Outbound Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `url` | string | **yes** | — | Webhook destination URL |
| `method` | string | optional | `POST` | HTTP method: `POST` or `PUT` |
| `secret` | string | optional | — | Secret for HMAC signing |
| `signature_header` | string | optional | `X-Webhook-Signature` | Header name for the signature |
| `signature_algorithm` | string | optional | `hmac-sha256` | Signing algorithm: `hmac-sha256`, `hmac-sha1`, `none` |
| `include_timestamp` | bool | optional | `true` | Include timestamp in signature computation |
| `timeout` | duration | optional | `30s` | Request timeout |
| `headers` | map | optional | — | Custom headers to include in every request |

### Retry Options (nested in `retry` block)

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `max_attempts` | int | optional | `3` | Maximum retry attempts |
| `initial_delay` | duration | optional | `1s` | Delay before first retry |
| `max_delay` | duration | optional | `30s` | Maximum delay between retries |
| `multiplier` | float | optional | `2.0` | Exponential backoff multiplier |

Retries on: 408, 429, 500, 502, 503, 504.

### Transform Fields

| Field | Type | Description |
|-------|------|-------------|
| `payload` | any | Request body (required) |
| `url` | string | Override destination URL |
| `method` | string | Override HTTP method |
| `event_type` | string | Value for `X-Webhook-Event` header |
| `idempotency_key` | string | Value for `X-Webhook-ID` header (defaults to UUID) |

### Automatic Headers

Every outbound webhook includes:
- `Content-Type: application/json`
- `User-Agent: Mycel-Webhook/1.0`
- `X-Webhook-ID: <UUID or idempotency_key>`
- `X-Webhook-Event: <event_type>` (if set)
- `X-Webhook-Signature: <HMAC>` (if secret is configured)

### Example

```hcl
flow "notify_partner" {
  from { connector = "rabbit", operation = "order.completed" }
  transform {
    payload    = "input.body"
    event_type = "'order.completed'"
  }
  to { connector = "webhooks_out", operation = "send" }
}

flow "receive_stripe" {
  from { connector = "webhooks_in", operation = "receive" }
  to   { connector = "db", target = "webhook_events" }
}
```

---

See the [notifications example](../../examples/notifications/) for a complete working setup.

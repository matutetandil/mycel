# Notifications Example

This example demonstrates how to use Mycel's notification connectors to send messages via different channels.

## Connectors Included

- **Email**: SMTP, SendGrid, AWS SES
- **Slack**: Webhook and Bot API
- **Discord**: Webhook and Bot API
- **SMS**: Twilio, AWS SNS
- **Push**: Firebase Cloud Messaging (FCM), Apple Push Notification service (APNs)
- **Webhooks**: Inbound/outbound with signature verification

## Configuration

Copy `service.example.hcl` to `service.hcl` and fill in your credentials:

```bash
cp service.example.hcl service.hcl
# Edit service.hcl with your actual credentials
```

## Running

```bash
mycel start --config ./examples/notifications
```

## Testing the API

### Send Email
```bash
curl -X POST http://localhost:8080/api/notify/email \
  -H "Content-Type: application/json" \
  -d '{
    "to": "user@example.com",
    "subject": "Test Email",
    "body": "Hello from Mycel!"
  }'
```

### Send Slack Message
```bash
curl -X POST http://localhost:8080/api/notify/slack \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "#general",
    "text": "Hello from Mycel!"
  }'
```

### Send Discord Message
```bash
curl -X POST http://localhost:8080/api/notify/discord \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Hello from Mycel!"
  }'
```

### Send SMS
```bash
curl -X POST http://localhost:8080/api/notify/sms \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+1234567890",
    "body": "Hello from Mycel!"
  }'
```

### Send Push Notification
```bash
curl -X POST http://localhost:8080/api/notify/push \
  -H "Content-Type: application/json" \
  -d '{
    "token": "device-token-here",
    "title": "Hello",
    "body": "Hello from Mycel!"
  }'
```

### Receive Webhook
```bash
# The webhook endpoint at /webhooks/events will receive and process incoming webhooks
curl -X POST http://localhost:8080/webhooks/events \
  -H "Content-Type: application/json" \
  -H "X-Signature-256: sha256=..." \
  -d '{"event": "order.created", "data": {"order_id": 123}}'
```

## Environment Variables

Set the following environment variables for your credentials:

```bash
# Email (SMTP)
export SMTP_HOST=smtp.gmail.com
export SMTP_PORT=587
export SMTP_USER=your-email@gmail.com
export SMTP_PASS=your-app-password

# Email (SendGrid)
export SENDGRID_API_KEY=SG.xxx

# Email (AWS SES)
export AWS_ACCESS_KEY_ID=xxx
export AWS_SECRET_ACCESS_KEY=xxx
export AWS_REGION=us-east-1

# Slack
export SLACK_WEBHOOK_URL=https://hooks.slack.com/services/xxx
export SLACK_BOT_TOKEN=xoxb-xxx

# Discord
export DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/xxx

# SMS (Twilio)
export TWILIO_ACCOUNT_SID=ACxxx
export TWILIO_AUTH_TOKEN=xxx
export TWILIO_FROM=+1234567890

# SMS (AWS SNS)
# Uses same AWS credentials as SES

# Push (FCM)
export FCM_SERVER_KEY=xxx

# Push (APNs)
export APNS_TEAM_ID=xxx
export APNS_KEY_ID=xxx
export APNS_PRIVATE_KEY=xxx
export APNS_BUNDLE_ID=com.example.app

# Webhooks
export WEBHOOK_SECRET=your-secret-key
```

## Flows

### notify_email
Receives notification requests and sends emails via the configured provider.

### notify_slack
Sends messages to Slack channels via webhook or Bot API.

### notify_discord
Sends messages to Discord channels.

### notify_sms
Sends SMS messages via Twilio or AWS SNS.

### notify_push
Sends push notifications via FCM or APNs.

### receive_webhook
Receives and validates incoming webhooks.

## Verify It Works

### 1. Start with mock mode (no real credentials needed)

```bash
mycel start --config ./examples/notifications --mock=email,slack,discord,sms,push
```

You should see:
```
INFO  Starting service: notifications-example
INFO  Loaded 6 connectors (5 mocked)
INFO  REST server listening on :8080
```

### 2. Test email notification (mocked)

```bash
curl -X POST http://localhost:8080/api/notify/email \
  -H "Content-Type: application/json" \
  -d '{"to": "user@example.com", "subject": "Test", "body": "Hello!"}'
```

Expected response:
```json
{
  "success": true,
  "provider": "mock",
  "message_id": "mock-12345"
}
```

### 3. Test Slack notification (mocked)

```bash
curl -X POST http://localhost:8080/api/notify/slack \
  -H "Content-Type: application/json" \
  -d '{"channel": "#general", "text": "Hello from Mycel!"}'
```

Expected response:
```json
{
  "success": true,
  "provider": "mock"
}
```

### 4. Test with real credentials

```bash
# Set real credentials
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/xxx"

# Start without mocking Slack
mycel start --config ./examples/notifications --no-mock=slack
```

Now Slack notifications are sent for real:
```bash
curl -X POST http://localhost:8080/api/notify/slack \
  -H "Content-Type: application/json" \
  -d '{"channel": "#testing", "text": "Real message!"}'
```

### What to check in logs

```
INFO  POST /api/notify/email
INFO    Provider: smtp (mocked)
INFO    To: user@example.com
INFO    Subject: Test
INFO  Response: 200 OK

INFO  POST /api/notify/slack
INFO    Provider: webhook
INFO    Channel: #general
INFO  Response: 200 OK
```

### Common Issues

**"Missing credentials for provider"**

Either set the required environment variables or use `--mock=<provider>`.

**"Webhook signature verification failed"**

For incoming webhooks, ensure `X-Signature-256` header is set correctly:
```bash
SIGNATURE=$(echo -n '{"event":"test"}' | openssl sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')
curl -X POST http://localhost:8080/webhooks/events \
  -H "X-Signature-256: sha256=$SIGNATURE" \
  -d '{"event":"test"}'
```

**"Rate limit exceeded"**

Notification providers have rate limits. For testing, use mock mode.

## See Also

- [Auth Example](../auth) - Send verification emails
- [MQ Example](../mq) - Process notification queue

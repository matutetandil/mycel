# Email notification request
type "email_request" {
  to       = string { required = true, format = "email" }
  name     = string { required = false }
  subject  = string { required = true }
  body     = string { required = true }
  html_body = string { required = false }
}

# Slack message request
type "slack_request" {
  channel = string { required = false }
  text    = string { required = true }
}

# Discord message request
type "discord_request" {
  content = string { required = true }
}

# SMS request
type "sms_request" {
  to   = string { required = true }
  body = string { required = true }
}

# Push notification request
type "push_request" {
  token = string { required = true }
  title = string { required = true }
  body  = string { required = true }
}

# Webhook event
type "webhook_event" {
  event = string { required = true }
}

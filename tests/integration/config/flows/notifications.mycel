# Notification flows (use HTTP client to call mock server)
# Note: Notification connectors (slack, discord, sms, push) don't implement
# connector.Writer, so we route through the HTTP client connector instead.

flow "send_slack" {
  from {
    connector = "api"
    operation = "POST /notify/slack"
  }
  transform {
    text    = "input.message"
    channel = "'#test'"
  }
  to {
    connector = "external_api"
    target    = "/slack/send"
    operation = "POST"
  }
}

flow "send_discord" {
  from {
    connector = "api"
    operation = "POST /notify/discord"
  }
  transform {
    content = "input.message"
  }
  to {
    connector = "external_api"
    target    = "/discord/send"
    operation = "POST"
  }
}

flow "send_sms" {
  from {
    connector = "api"
    operation = "POST /notify/sms"
  }
  transform {
    to      = "input.phone"
    message = "input.message"
  }
  to {
    connector = "external_api"
    target    = "/sms/send"
    operation = "POST"
  }
}

flow "send_push" {
  from {
    connector = "api"
    operation = "POST /notify/push"
  }
  transform {
    token   = "input.device_token"
    title   = "input.title"
    message = "input.message"
  }
  to {
    connector = "external_api"
    target    = "/push/send"
    operation = "POST"
  }
}

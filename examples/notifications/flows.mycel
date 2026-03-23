# Send Email Flow
flow "notify_email" {
  from {
    connector = "api"
    operation = "POST /api/notify/email"
  }

  to {
    connector = "email_smtp"
    operation = "send"
  }

  transform {
    to       = "[{'email': input.to, 'name': input.name ?? ''}]"
    subject  = "input.subject"
    textBody = "input.body"
    htmlBody = "input.html_body ?? ''"
  }
}

# Send Slack Message Flow
flow "notify_slack" {
  from {
    connector = "api"
    operation = "POST /api/notify/slack"
  }

  to {
    connector = "slack"
    operation = "send"
  }

  transform {
    channel = "input.channel ?? '#notifications'"
    text    = "input.text"
  }
}

# Send Discord Message Flow
flow "notify_discord" {
  from {
    connector = "api"
    operation = "POST /api/notify/discord"
  }

  to {
    connector = "discord"
    operation = "send"
  }

  transform {
    content = "input.content"
  }
}

# Send SMS Flow
flow "notify_sms" {
  from {
    connector = "api"
    operation = "POST /api/notify/sms"
  }

  to {
    connector = "sms"
    operation = "send"
  }

  transform {
    to   = "input.to"
    body = "input.body"
  }
}

# Send Push Notification Flow
flow "notify_push" {
  from {
    connector = "api"
    operation = "POST /api/notify/push"
  }

  to {
    connector = "push"
    operation = "send"
  }

  transform {
    token = "input.token"
    title = "input.title"
    body  = "input.body"
    data  = "input.data ?? {}"
  }
}

# Receive Webhook Flow
flow "receive_webhook" {
  from {
    connector = "webhooks_in"
    operation = "receive"
  }

  to {
    connector = "slack"
    operation = "send"
  }

  transform {
    channel = "'#webhook-events'"
    text    = "'Received webhook: ' + input.event + ' - ' + string(input.data)"
  }
}

# Forward to External Webhook Flow
flow "forward_webhook" {
  from {
    connector = "api"
    operation = "POST /api/forward"
  }

  to {
    connector = "webhooks_out"
    operation = "send"
  }

  transform {
    event   = "input.event"
    payload = "input"
  }
}

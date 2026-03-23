# Notification connectors (all pointing to mock server)

# Slack (via mock)
connector "slack" {
  type        = "slack"
  webhook_url = env("MOCK_URL", "http://mock:8888")
  channel     = "#test"
  username    = "Mycel Test"
  api_url     = env("MOCK_URL", "http://mock:8888")
}

# Discord (via mock)
connector "discord" {
  type        = "discord"
  webhook_url = env("MOCK_URL", "http://mock:8888")
  username    = "Mycel Test"
  api_url     = env("MOCK_URL", "http://mock:8888")
}

# SMS (via mock)
connector "sms" {
  type        = "sms"
  driver      = "twilio"
  account_sid = "AC_test_sid"
  auth_token  = "test_token"
  from        = "+15551234567"
  api_url     = env("MOCK_URL", "http://mock:8888")
}

# Push (via mock)
connector "push" {
  type       = "push"
  driver     = "fcm"
  server_key = "test_server_key"
  api_url    = env("MOCK_URL", "http://mock:8888")
}

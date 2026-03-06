# User type
type "user" {
  id    = number({ min = 0, required = false })
  name  = string
  email = string
}

# Item type
type "item" {
  title  = string
  status = string
}

# MQ message type
type "mq_message" {
  source  = string
  payload = string
}

# Cache entry type
type "cache_entry" {
  key   = string
  value = string
}

# Notification type
type "notification" {
  type    = string
  message = string
}

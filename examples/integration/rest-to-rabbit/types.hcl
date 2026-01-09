# Types for REST -> RabbitMQ integration
# NOTE: Complex type constraints are simplified for this example

type "job_request" {
  type     = string
  params   = object
  priority = string
}

type "order_input" {
  order_id = string
  customer = object
  items    = array
  total    = number
}

type "webhook_event" {
  type      = string
  timestamp = string
  data      = object
  signature = string
}

type "batch_events" {
  events = array
}

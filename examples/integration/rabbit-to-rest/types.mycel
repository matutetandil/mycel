# Types for RabbitMQ -> REST integration
# NOTE: Complex type constraints are simplified for this example

type "order_message" {
  order_id         = string
  customer         = object
  items            = array
  shipping_address = object
  priority         = string
}

type "status_update" {
  order_id  = string
  status    = string
  timestamp = string
  notes     = string
}

type "customer_sync" {
  external_crm_id = string
  email           = string
  first_name      = string
  last_name       = string
  phone           = string
  company         = string
}

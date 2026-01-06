# Types for RabbitMQ -> REST integration

type "order_message" {
  order_id = string { required = true }
  customer = object {
    id               = string { required = true }
    email            = string { format = "email" }
    preferred_channel = string { enum = ["email", "sms", "push"] }
  }
  items = array {
    item = object {
      sku      = string { required = true }
      quantity = number { min = 1 }
      price    = number { min = 0 }
    }
  }
  shipping_address = object {
    street  = string { required = true }
    city    = string { required = true }
    state   = string
    zip     = string { required = true }
    country = string { required = true }
  }
  priority = string { enum = ["standard", "express"] }
}

type "status_update" {
  order_id = string { required = true }
  status   = string {
    required = true
    enum     = ["pending", "processing", "shipped", "delivered", "cancelled"]
  }
  timestamp = string { format = "datetime" }
  notes     = string { optional = true }
}

type "customer_sync" {
  external_crm_id = string { required = true }
  email           = string { format = "email", required = true }
  first_name      = string { required = true }
  last_name       = string { required = true }
  phone           = string { optional = true }
  company         = string { optional = true }
}

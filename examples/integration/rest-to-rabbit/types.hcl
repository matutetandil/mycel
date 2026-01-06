# Types for REST -> RabbitMQ integration

type "job_request" {
  type = string {
    required = true
    enum     = ["report", "export", "import", "cleanup", "sync"]
  }
  params = object {
    optional = true
  }
  priority = string {
    optional = true
    enum     = ["low", "normal", "high"]
  }
}

type "order_input" {
  order_id = string { optional = true }
  customer = object {
    id    = string { required = true }
    email = string { format = "email" }
    name  = string { required = true }
  }
  items = array {
    item = object {
      sku      = string { required = true }
      quantity = number { min = 1, required = true }
      price    = number { min = 0, required = true }
    }
    min_items = 1
  }
  total = number { min = 0, required = true }
}

type "webhook_event" {
  type      = string { required = true }
  timestamp = string { format = "datetime" }
  data      = object { required = true }
  signature = string { optional = true }
}

type "batch_events" {
  events = array {
    event = object {
      id        = string { optional = true }
      type      = string { required = true }
      data      = object { required = true }
      timestamp = string { optional = true }
      source    = string { optional = true }
    }
    min_items = 1
    max_items = 1000
  }
}

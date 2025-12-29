// Type Definitions for Message Queue Example

type "order_request" {
  field "product" {
    type     = "string"
    required = true
    min      = 1
    max      = 100
  }

  field "quantity" {
    type     = "integer"
    required = true
    min      = 1
  }

  field "customer" {
    type     = "object"
    required = true

    field "name" {
      type     = "string"
      required = true
    }

    field "email" {
      type     = "string"
      required = true
      pattern  = "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
    }
  }
}

type "order_event" {
  field "order_id" {
    type     = "string"
    required = true
  }

  field "product" {
    type     = "string"
    required = true
  }

  field "quantity" {
    type     = "integer"
    required = true
  }

  field "customer" {
    type     = "object"
    required = true
  }

  field "status" {
    type     = "string"
    required = true
    enum     = ["pending", "processing", "completed", "cancelled"]
  }

  field "created_at" {
    type     = "string"
    required = true
  }
}

type "notification" {
  field "notification_type" {
    type     = "string"
    required = true
    enum     = ["email", "sms", "push"]
  }

  field "recipient" {
    type     = "string"
    required = true
  }

  field "subject" {
    type = "string"
  }

  field "message" {
    type     = "string"
    required = true
  }
}

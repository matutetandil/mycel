// Type definitions for the checkout flow

type "order_item" {
  name     = string
  price    = number
  quantity = number
}

type "checkout_request" {
  items            = list
  discount_percent = number
  shipping_country = string
  email            = string
}

type "order" {
  order_id       = string
  subtotal       = number
  discount       = number
  tax            = number
  total          = number
  customer_email = string
  created_at     = string
}

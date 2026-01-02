// Flow definitions using WASM functions

// Checkout flow - demonstrates WASM pricing functions
flow "checkout" {
  from {
    connector = "api"
    operation = "POST /checkout"
  }

  transform {
    // Use WASM functions from pricing module
    subtotal = "calculate_price(input.items)"
    discount = "apply_discount(subtotal, input.discount_percent)"
    tax      = "tax_for_country(discount, input.shipping_country)"
    total    = "discount + tax"

    // Mix with built-in CEL functions
    order_id       = "uuid()"
    created_at     = "now()"
    customer_email = "lower(input.email)"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

// Get orders
flow "get_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}

// Get single order
flow "get_order" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}

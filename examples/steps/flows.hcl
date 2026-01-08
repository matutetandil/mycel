# Steps Example - Flow Orchestration
# Demonstrates multi-step flows with intermediate connector calls.
#
# Key concepts:
# - Steps execute in order before the transform
# - Each step's result is available as step.<name>.* in subsequent steps and transforms
# - Steps support conditional execution with "when" clause
# - Steps support error handling with on_error (skip, fail, default)

# =============================================================================
# Example 1: Basic Multi-Step Flow
# =============================================================================
# Create an order by:
# 1. Looking up the user
# 2. Looking up the product
# 3. Calculating total with tax
# 4. Creating the order record

flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  # Step 1: Get user details
  step "user" {
    connector = "db"
    query     = "SELECT * FROM users WHERE id = :user_id"
    params = {
      user_id = "input.user_id"
    }
  }

  # Step 2: Get product details
  step "product" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :product_id"
    params = {
      product_id = "input.product_id"
    }
  }

  # Step 3: Get pricing (from pricing service)
  step "pricing" {
    connector = "pricing_db"
    query     = "SELECT price, tax_rate FROM prices WHERE product_id = :product_id"
    params = {
      product_id = "input.product_id"
    }
  }

  # Transform combines all step results
  transform {
    id           = "uuid()"
    user_id      = "step.user.id"
    user_email   = "step.user.email"
    product_id   = "step.product.id"
    product_name = "step.product.name"
    quantity     = "input.quantity"
    unit_price   = "step.pricing.price"
    tax_rate     = "step.pricing.tax_rate"
    subtotal     = "double(input.quantity) * step.pricing.price"
    tax          = "double(input.quantity) * step.pricing.price * step.pricing.tax_rate"
    total        = "double(input.quantity) * step.pricing.price * (1.0 + step.pricing.tax_rate)"
    created_at   = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

# =============================================================================
# Example 2: Conditional Steps
# =============================================================================
# Get product details with optional pricing and inventory data.
# Use "when" to conditionally execute steps based on input.

flow "get_product_details" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  # Always get product
  step "product" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :id"
    params = {
      id = "input.id"
    }
  }

  # Only get pricing if requested
  step "pricing" {
    connector = "pricing_db"
    query     = "SELECT price, currency FROM prices WHERE product_id = :product_id"
    params = {
      product_id = "input.id"
    }
    when      = "input.include_price == true"
    on_error  = "default"
    default   = { price = 0, currency = "USD" }
  }

  # Only get inventory if requested
  step "inventory" {
    connector = "inventory_db"
    query     = "SELECT available, reserved FROM inventory WHERE product_id = :product_id"
    params = {
      product_id = "input.id"
    }
    when     = "input.include_inventory == true"
    on_error = "skip"
  }

  transform {
    id          = "step.product.id"
    name        = "step.product.name"
    description = "step.product.description"
    price       = "step.pricing.price"
    currency    = "step.pricing.currency"
    available   = "step.inventory.available"
    in_stock    = "step.inventory.available > 0"
  }

  to {
    connector = "db"
    target    = "products"
  }
}

# =============================================================================
# Example 3: Chained Steps
# =============================================================================
# Steps can reference results from previous steps.
# Get order details with user, product, and shipping info.

flow "get_order_details" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  # Step 1: Get the order
  step "order" {
    connector = "db"
    query     = "SELECT * FROM orders WHERE id = :id"
    params = {
      id = "input.id"
    }
  }

  # Step 2: Get user using user_id from order (chained)
  step "user" {
    connector = "db"
    query     = "SELECT * FROM users WHERE id = :user_id"
    params = {
      user_id = "step.order.user_id"  # References previous step result!
    }
  }

  # Step 3: Get product using product_id from order (chained)
  step "product" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :product_id"
    params = {
      product_id = "step.order.product_id"  # References previous step result!
    }
  }

  transform {
    order_id     = "step.order.id"
    order_status = "step.order.status"
    order_total  = "step.order.total"
    user_name    = "step.user.name"
    user_email   = "step.user.email"
    product_name = "step.product.name"
    created_at   = "step.order.created_at"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

# =============================================================================
# Example 4: Error Handling
# =============================================================================
# Demonstrate different error handling strategies for steps.

flow "process_payment" {
  from {
    connector = "api"
    operation = "POST /payments"
  }

  # Step 1: Validate the order exists (required)
  step "order" {
    connector = "db"
    query     = "SELECT * FROM orders WHERE id = :order_id AND status = 'pending'"
    params = {
      order_id = "input.order_id"
    }
    on_error = "fail"  # Fail the entire flow if order not found
  }

  # Step 2: Get user's payment methods (optional, use default)
  step "payment_methods" {
    connector = "db"
    query     = "SELECT * FROM payment_methods WHERE user_id = :user_id AND is_default = true"
    params = {
      user_id = "step.order.user_id"
    }
    on_error = "default"
    default = {
      type     = "credit_card"
      provider = "stripe"
    }
  }

  # Step 3: Check fraud score (optional, skip if fails)
  step "fraud_check" {
    connector = "db"
    query     = "SELECT risk_score FROM fraud_scores WHERE user_id = :user_id"
    params = {
      user_id = "step.order.user_id"
    }
    on_error = "skip"  # Skip fraud check if service unavailable
    timeout  = "5s"
  }

  transform {
    id               = "uuid()"
    order_id         = "step.order.id"
    amount           = "step.order.total"
    payment_type     = "step.payment_methods.type"
    payment_provider = "step.payment_methods.provider"
    risk_score       = "step.fraud_check.risk_score"
    status           = "'processing'"
    created_at       = "now()"
  }

  to {
    connector = "db"
    target    = "payments"
  }
}

# =============================================================================
# Example 5: Request Filtering
# =============================================================================
# Use filter in from block to skip certain requests before processing.
# Filter is a CEL expression that must evaluate to true for processing.

flow "process_external_orders" {
  from {
    connector = "api"
    operation = "POST /external-orders"
    # Only process orders from external sources (not internal)
    filter    = "input.metadata.origin != 'internal'"
  }

  step "validate" {
    connector = "db"
    query     = "SELECT 1 FROM allowed_sources WHERE source = :source"
    params = {
      source = "input.metadata.origin"
    }
    on_error = "fail"
  }

  transform {
    id         = "uuid()"
    source     = "input.metadata.origin"
    payload    = "input.data"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "external_orders"
  }
}

# Another filter example: Only process high-value orders
flow "process_high_value_orders" {
  from {
    connector = "api"
    operation = "POST /orders/high-value"
    # Only process orders with total >= 1000
    filter    = "input.total >= 1000"
  }

  step "user" {
    connector = "db"
    query     = "SELECT * FROM vip_users WHERE id = :user_id"
    params = {
      user_id = "input.user_id"
    }
  }

  transform {
    id           = "uuid()"
    user_id      = "input.user_id"
    total        = "input.total"
    is_vip       = "step.user.id != null"
    priority     = "'high'"
    processed_at = "now()"
  }

  to {
    connector = "db"
    target    = "high_value_orders"
  }
}

# =============================================================================
# Example 6: Array Transforms
# =============================================================================
# Demonstrate array helper functions for data manipulation.
# Available functions: first, last, unique, reverse, flatten, pluck,
#                      sum, avg, min_val, max_val, sort_by

flow "get_order_summary" {
  from {
    connector = "api"
    operation = "GET /orders/:user_id/summary"
  }

  # Step 1: Get all orders for the user
  step "orders" {
    connector = "db"
    query     = "SELECT * FROM orders WHERE user_id = :user_id"
    params = {
      user_id = "input.user_id"
    }
  }

  # Step 2: Get all order items
  step "items" {
    connector = "db"
    query     = "SELECT * FROM order_items WHERE order_id IN (:order_ids)"
    params = {
      order_ids = "pluck(step.orders, 'id')"
    }
  }

  transform {
    user_id         = "input.user_id"
    total_orders    = "size(step.orders)"
    total_spent     = "sum(pluck(step.orders, 'total'))"
    average_order   = "avg(pluck(step.orders, 'total'))"
    min_order       = "min_val(pluck(step.orders, 'total'))"
    max_order       = "max_val(pluck(step.orders, 'total'))"
    first_order     = "first(sort_by(step.orders, 'created_at'))"
    last_order      = "last(sort_by(step.orders, 'created_at'))"
    unique_products = "unique(pluck(step.items, 'product_id'))"
    product_count   = "size(unique(pluck(step.items, 'product_id')))"
  }

  to {
    connector = "db"
    target    = "order_summaries"
  }
}

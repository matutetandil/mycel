# Multi-step flow with on_error = "skip" for non-critical steps
#
# Fetches an order from the database, then enriches it with data
# from external services. If enrichment fails, the flow continues
# with partial data instead of failing entirely.

flow "get_order_details" {
  from {
    connector = "api"
    operation = "GET /orders/:id/details"
  }

  # Step 1: Get the order (required - fails the whole flow if missing)
  step "order" {
    connector = "postgres"
    query     = "SELECT * FROM orders WHERE id = :id"
    params    = { id = "input.id" }
    on_error  = "fail"
  }

  # Step 2: Enrich with customer info (optional - skip if unavailable)
  step "customer" {
    connector = "postgres"
    query     = "SELECT name, email, tier FROM customers WHERE id = :id"
    params    = { id = "step.order.customer_id" }
    on_error  = "skip"
  }

  # Step 3: Get shipping estimate from external service (optional with default)
  step "shipping" {
    connector = "postgres"
    query     = "SELECT estimated_days, carrier FROM shipping_estimates WHERE order_id = :id"
    params    = { id = "input.id" }
    on_error  = "default"
    default   = { estimated_days = 7, carrier = "standard" }
  }

  # Step 4: Get loyalty points (optional - skip entirely if service is down)
  step "loyalty" {
    connector = "postgres"
    query     = "SELECT points, tier FROM loyalty WHERE customer_id = :id"
    params    = { id = "step.order.customer_id" }
    on_error  = "skip"
    timeout   = "3s"
  }

  transform {
    output.id             = "step.order.id"
    output.product_id     = "step.order.product_id"
    output.quantity        = "step.order.quantity"
    output.status         = "step.order.status"
    output.customer_name  = "step.customer.name"
    output.customer_email = "step.customer.email"
    output.shipping_days  = "step.shipping.estimated_days"
    output.carrier        = "step.shipping.carrier"
    output.loyalty_points = "step.loyalty.points"
  }
}

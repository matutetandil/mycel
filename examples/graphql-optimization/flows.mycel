# Flows demonstrating GraphQL Query Optimization
#
# These flows look like standard Mycel flows, but Mycel automatically:
# 1. Rewrites SELECT * to only fetch requested columns
# 2. Skips steps whose output fields aren't requested
# 3. Batches N+1 queries via DataLoader

# =============================================================================
# OPTIMIZATION 1: Field Selection
# =============================================================================
# When a client requests only { id, name }, Mycel executes:
#   SELECT id, name FROM users (not SELECT *)
#
# This reduces:
# - Network bandwidth (fewer bytes transferred)
# - Database load (fewer columns to read)
# - Memory usage (smaller result sets)

flow "get_users" {
  from {
    connector = "api"
    operation = "Query.users"
  }

  to {
    connector = "db"
    target    = "users"
  }

  returns = "User[]"
}

flow "get_user" {
  from {
    connector = "api"
    operation = "Query.user"
  }

  to {
    connector = "db"
    target    = "users"
  }

  returns = "User"
}

# =============================================================================
# OPTIMIZATION 2: Step Skipping
# =============================================================================
# This flow has 3 steps that fetch data from external services.
# If a client only requests { id, name, description }, all 3 steps are SKIPPED!
# If a client requests { id, name, price }, only the pricing step executes.
#
# The magic: Mycel analyzes the transform expressions to determine which
# steps are needed based on the requested fields.

flow "get_product" {
  from {
    connector = "api"
    operation = "Query.product"
  }

  # Step 1: Get base product data from database
  step "base" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :id"
    params    = { id = "input.id" }
  }

  # Step 2: Get pricing from external API (SKIPPED if price not requested)
  step "pricing" {
    connector = "pricing_api"
    query     = "SELECT * FROM prices WHERE product_id = :id"
    params    = { id = "input.id" }
    on_error  = "skip"  # Don't fail if pricing unavailable
  }

  # Step 3: Get inventory from external API (SKIPPED if stock not requested)
  step "inventory" {
    connector = "inventory_api"
    query     = "SELECT * FROM inventory WHERE product_id = :id"
    params    = { id = "input.id" }
    on_error  = "skip"
  }

  # Step 4: Get reviews from external API (SKIPPED if rating/reviewCount not requested)
  step "reviews" {
    connector = "reviews_api"
    query     = "SELECT product_id, AVG(rating) as rating, COUNT(*) as review_count FROM reviews WHERE product_id = :id GROUP BY product_id"
    params    = { id = "input.id" }
    on_error  = "skip"
  }

  transform {
    # Base fields from database
    id          = "step.base.id"
    sku         = "step.base.sku"
    name        = "step.base.name"
    description = "step.base.description"
    category    = "step.base.category"

    # Fields from external services (triggers step execution)
    price       = "step.pricing != null ? step.pricing.price : 0"
    stock       = "step.inventory != null ? step.inventory.stock : 0"
    rating      = "step.reviews != null ? step.reviews.rating : 0"
    reviewCount = "step.reviews != null ? step.reviews.review_count : 0"
  }

  # Destination (required, but transform result is returned)
  to {
    connector = "db"
    target    = "products"
  }

  returns = "Product"
}

# List products with the same optimization
flow "get_products" {
  from {
    connector = "api"
    operation = "Query.products"
  }

  to {
    connector = "db"
    target    = "products"
  }

  returns = "Product[]"
}

# =============================================================================
# OPTIMIZATION 3: DataLoader (N+1 Prevention)
# =============================================================================
# When fetching orders with nested user/product data:
#
# WITHOUT DataLoader (N+1 problem):
#   SELECT * FROM orders                    -- 1 query
#   SELECT * FROM users WHERE id = 1        -- N queries (one per order)
#   SELECT * FROM products WHERE id = 101
#   SELECT * FROM users WHERE id = 2
#   SELECT * FROM products WHERE id = 102
#   ... (10 orders = 21 queries!)
#
# WITH DataLoader (Mycel automatic):
#   SELECT * FROM orders                    -- 1 query
#   SELECT * FROM users WHERE id IN (1,2,3) -- 1 batched query
#   SELECT * FROM products WHERE id IN (...) -- 1 batched query
#   Total: 3 queries instead of 21!

flow "get_orders" {
  from {
    connector = "api"
    operation = "Query.orders"
  }

  to {
    connector = "db"
    target    = "orders"
  }

  returns = "Order[]"
}

flow "get_order" {
  from {
    connector = "api"
    operation = "Query.order"
  }

  to {
    connector = "db"
    target    = "orders"
  }

  returns = "Order"
}

# =============================================================================
# OPTIMIZATION 4: Conditional Steps with CEL
# =============================================================================
# Use has_field() to explicitly control step execution based on requested fields.
# This is useful when you want more control over the optimization logic.

flow "get_product_details" {
  from {
    connector = "api"
    operation = "Query.productDetails"
  }

  step "base" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :id"
    params    = { id = "input.id" }
  }

  # Only fetch pricing if price field is requested
  step "pricing" {
    when      = "has_field(input, 'price') || has_field(input, 'discount')"
    connector = "pricing_api"
    query     = "SELECT * FROM prices WHERE product_id = :id"
    params    = { id = "input.id" }
  }

  # Only fetch inventory if stock-related fields are requested
  step "inventory" {
    when      = "has_field(input, 'stock') || has_field(input, 'warehouse')"
    connector = "inventory_api"
    query     = "SELECT * FROM inventory WHERE product_id = :id"
    params    = { id = "input.id" }
  }

  transform {
    id          = "step.base.id"
    name        = "step.base.name"
    description = "step.base.description"
    price       = "step.pricing != null ? step.pricing.price : null"
    discount    = "step.pricing != null ? step.pricing.discount : null"
    stock       = "step.inventory != null ? step.inventory.stock : null"
    warehouse   = "step.inventory != null ? step.inventory.warehouse : null"
  }

  # Destination (required, but transform result is returned)
  to {
    connector = "db"
    target    = "products"
  }

  returns = "Product"
}

# =============================================================================
# Mutations (standard, no special optimization needed)
# =============================================================================

flow "create_user" {
  from {
    connector = "api"
    operation = "Mutation.createUser"
  }

  to {
    connector  = "db"
    target     = "users"
    operation  = "INSERT"
  }

  returns = "User"
}

flow "create_order" {
  from {
    connector = "api"
    operation = "Mutation.createOrder"
  }

  to {
    connector  = "db"
    target     = "orders"
    operation  = "INSERT"
  }

  returns = "Order"
}

# GraphQL Federation Flows
# Demonstrates queries, mutations, entity resolvers, and subscriptions
# within a federated GraphQL subgraph.

# =============================================================================
# QUERIES
# =============================================================================

# Query: List all products
# GraphQL: { products { sku name price category } }

flow "list_products" {
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

# Query: Get a single product by SKU
# GraphQL: { product(sku: "ABC-123") { sku name price inStock } }

flow "get_product" {
  from {
    connector = "api"
    operation = "Query.product"
  }

  to {
    connector = "db"
    target    = "products"
    query     = "SELECT * FROM products WHERE sku = :sku"
  }

  returns = "Product"
}

# Query: List reviews for a product
# GraphQL: { reviews(productSku: "ABC-123") { id rating comment author } }

flow "list_reviews" {
  from {
    connector = "api"
    operation = "Query.reviews"
  }

  to {
    connector = "db"
    target    = "reviews"
    query     = "SELECT * FROM reviews WHERE product_sku = :productSku"
  }

  returns = "Review[]"
}

# =============================================================================
# ENTITY RESOLVERS (Federation)
# =============================================================================
# Entity resolvers allow other subgraphs to reference Product by its @key.
# When the gateway receives a query that spans subgraphs, it calls
# _entities with representations like { __typename: "Product", sku: "ABC-123" }.
# Mycel resolves these automatically via the entity flow.

flow "resolve_product" {
  entity = "Product"

  from {
    connector = "api"
    operation = "Query._resolveProduct"
  }

  to {
    connector = "db"
    target    = "products"
    query     = "SELECT * FROM products WHERE sku = :sku"
  }

  returns = "Product"
}

flow "resolve_review" {
  entity = "Review"

  from {
    connector = "api"
    operation = "Query._resolveReview"
  }

  to {
    connector = "db"
    target    = "reviews"
    query     = "SELECT * FROM reviews WHERE id = :id"
  }

  returns = "Review"
}

# =============================================================================
# MUTATIONS
# =============================================================================

# Mutation: Create a new product
# GraphQL: mutation { createProduct(input: { sku: "ABC-123", name: "Widget", price: 19.99 }) { sku name price } }

flow "create_product" {
  from {
    connector = "api"
    operation = "Mutation.createProduct"
  }

  transform {
    sku         = "input.input.sku"
    name        = "input.input.name"
    price       = "input.input.price"
    description = "input.input.description"
    category    = "input.input.category"
    in_stock    = "true"
    created_at  = "now()"
    updated_at  = "now()"
  }

  to {
    connector = "db"
    target    = "products"
  }

  returns = "Product"
}

# Mutation: Update product price
# GraphQL: mutation { updateProductPrice(sku: "ABC-123", price: 24.99) { sku name price updatedAt } }

flow "update_product_price" {
  from {
    connector = "api"
    operation = "Mutation.updateProductPrice"
  }

  transform {
    sku        = "input.sku"
    price      = "input.price"
    updated_at = "now()"
  }

  to {
    connector = "db"
    target    = "products"
    query     = "UPDATE products SET price = :price, updated_at = :updated_at WHERE sku = :sku"
  }

  returns = "Product"
}

# Mutation: Add a review to a product
# GraphQL: mutation { addReview(input: { productSku: "ABC-123", rating: 5, comment: "Great!", author: "Alice" }) { id rating } }

flow "add_review" {
  from {
    connector = "api"
    operation = "Mutation.addReview"
  }

  transform {
    id          = "uuid()"
    product_sku = "input.input.productSku"
    rating      = "input.input.rating"
    comment     = "input.input.comment"
    author      = "input.input.author"
    created_at  = "now()"
  }

  to {
    connector = "db"
    target    = "reviews"
  }

  returns = "Review"
}

# =============================================================================
# SUBSCRIPTIONS
# =============================================================================
# Subscriptions push real-time updates to connected clients.
# This flow listens for product update events on a message queue and
# publishes them to the GraphQL subscription.

# Subscription: Product updated (triggered by queue events)
# GraphQL: subscription { productUpdated { sku name price updatedAt } }
#
# When a message arrives on the "product.updated" routing key,
# it is forwarded to all clients subscribed to productUpdated.

flow "product_updated_sub" {
  from {
    connector = "events"
    operation = "product.updated"
  }

  transform {
    sku       = "input.sku"
    name      = "input.name"
    price     = "input.price"
    updatedAt = "input.updatedAt"
  }

  to {
    connector = "api"
    operation = "Subscription.productUpdated"
  }

  returns = "Product"
}

# Subscription: New review added (triggered by queue events)
# GraphQL: subscription { reviewAdded(productSku: "ABC-123") { id rating comment author } }

flow "review_added_sub" {
  from {
    connector = "events"
    operation = "product.review"
  }

  transform {
    id         = "input.id"
    productSku = "input.productSku"
    rating     = "input.rating"
    comment    = "input.comment"
    author     = "input.author"
    createdAt  = "input.createdAt"
  }

  to {
    connector = "api"
    operation = "Subscription.reviewAdded"
    filter    = "input.productSku == context.args.productSku"
  }

  returns = "Review"
}

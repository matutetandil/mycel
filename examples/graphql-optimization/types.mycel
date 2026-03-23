# Type definitions for GraphQL Optimization Demo

# User type - demonstrates field selection optimization
type "User" {
  id        = string
  email     = string
  name      = string
  avatar    = string
  bio       = string
  createdAt = string
  updatedAt = string
}

# Product type - demonstrates step skipping optimization
type "Product" {
  id          = string
  sku         = string
  name        = string
  description = string
  category    = string

  # These fields come from external APIs (steps)
  # If not requested, the step is skipped automatically!
  price       = number   # From pricing_api step
  stock       = number   # From inventory_api step
  rating      = number   # From reviews_api step
  reviewCount = number   # From reviews_api step
}

# Order type - demonstrates DataLoader for N+1 prevention
type "Order" {
  id        = string
  userId    = string
  productId = string
  quantity  = number
  total     = number
  status    = string
  createdAt = string

  # Nested types - would cause N+1 without DataLoader
  user    = User
  product = Product
}

# Price type (from pricing service)
type "Price" {
  productId = string
  price     = number
  currency  = string
  discount  = number
}

# Inventory type (from inventory service)
type "Inventory" {
  productId = string
  stock     = number
  warehouse = string
  reserved  = number
}

# Review summary type
type "ReviewSummary" {
  productId   = string
  rating      = number
  reviewCount = number
}

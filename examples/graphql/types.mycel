# GraphQL Types with Federation Directives Example
# This demonstrates HCL-first mode with federation support

# Basic User type with @key directive for federation
type "User" {
  _key         = "id"
  _shareable   = true
  _description = "A user entity in the federated graph"

  id    = string
  email = string
  name  = string
}

# Product type with multiple keys (compound key federation)
type "Product" {
  _key         = ["sku", "sku region"]
  _description = "Product entity with compound federation keys"

  sku       = string
  region    = string
  name      = string
  price     = number
  inventory = number
}

# Order type with federation field directives
type "Order" {
  _key         = "id"
  _description = "Order entity with external references"

  id     = string
  total  = number
  status = string
}

# Review type extending another subgraph's type
type "Review" {
  _key         = "id"
  _description = "Product review"

  id        = string
  rating    = number
  comment   = string
  productId = string
}

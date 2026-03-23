# GraphQL Federation Types
# Types use underscore-prefixed attributes for Federation v2 directives.
#
# Mapping:
#   _key       -> @key(fields: "...")
#   _shareable -> @shareable
#   _external  -> field-level @external (on individual fields)

# Product entity - the primary type exposed by this subgraph.
# Other subgraphs can reference Product by its key field (sku).
type "Product" {
  _key       = "sku"
  _shareable = true

  sku         = string { required = true }
  name        = string { required = true }
  price       = number { min = 0 }
  description = string
  category    = string
  inStock     = boolean
  createdAt   = string
  updatedAt   = string
}

# Review entity - belongs to a product.
# Other subgraphs can extend Review or reference it by its key (id).
type "Review" {
  _key = "id"

  id         = string { required = true }
  productSku = string { required = true }
  rating     = number { min = 1, max = 5 }
  comment    = string
  author     = string
  createdAt  = string
}

# Input type for creating a product
type "ProductInput" {
  sku         = string { required = true }
  name        = string { required = true }
  price       = number { min = 0 }
  description = string
  category    = string
}

# Input type for creating a review
type "ReviewInput" {
  productSku = string { required = true }
  rating     = number { min = 1, max = 5, required = true }
  comment    = string
  author     = string { required = true }
}

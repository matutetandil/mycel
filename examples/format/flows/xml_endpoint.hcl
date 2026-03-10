# XML endpoint - flow-level format override
# Accepts XML requests, returns XML responses

# List products as XML
flow "get_products_xml" {
  from {
    connector = "api"
    operation = "GET /products.xml"
    format    = "xml"
  }

  to {
    connector = "sqlite"
    target    = "products"
  }
}

# Create product from XML body
flow "create_product_xml" {
  from {
    connector = "api"
    operation = "POST /products.xml"
    format    = "xml"
  }

  to {
    connector = "sqlite"
    target    = "products"
  }
}

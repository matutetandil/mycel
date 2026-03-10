# Mixed format flow - JSON input, XML enrichment step, JSON output
# Demonstrates step-level format declarations

flow "enrich_product" {
  from {
    connector = "api"
    operation = "POST /products/enrich"
  }

  # Step 1: Save to local database
  step "save" {
    connector = "sqlite"
    target    = "products"
  }

  # Step 2: Enrich with data from legacy SOAP API (XML)
  # The connector already has format = "xml", but you can also set it per step
  step "get_legacy_info" {
    connector = "soap_api"
    operation = "GET /products/${step.save.id}/details"
    format    = "xml"
  }

  # Step 3: Merge and return as JSON (default)
  transform {
    output.id          = step.save.id
    output.name        = step.save.name
    output.price       = step.save.price
    output.legacy_code = step.get_legacy_info.product_code
    output.warehouse   = step.get_legacy_info.warehouse
  }
}

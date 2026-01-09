# Reusable transforms
# NOTE: enrich blocks are a planned feature (not yet implemented)

# Simple transform without enrichment
transform "normalize_product" {
  id       = "input.id"
  name     = "upper(input.name)"
  sku      = "input.sku"
}

# Transform with calculated fields
transform "with_metadata" {
  id         = "input.id"
  name       = "input.name"
  fetched_at = "now()"
  source     = "'local'"
}

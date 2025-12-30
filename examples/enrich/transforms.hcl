# Reusable transforms with enrichment

# Transform that always enriches with pricing
# Any flow using this transform will automatically get pricing data
transform "with_pricing" {
  # Enrich block inside named transform
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  # Mappings that use the enriched data
  id       = "input.id"
  name     = "input.name"
  price    = "enriched.pricing.price"
  currency = "enriched.pricing.currency"
}

# Transform with multiple enrichments
transform "with_full_data" {
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  enrich "inventory" {
    connector = "inventory_service"
    operation = "GET /stock"
    params {
      sku = "input.sku"
    }
  }

  # Build full product response
  id       = "input.id"
  name     = "upper(input.name)"
  sku      = "input.sku"
  price    = "enriched.pricing.price"
  currency = "enriched.pricing.currency"
  stock    = "enriched.inventory.available"
  in_stock = "enriched.inventory.available > 0"
}

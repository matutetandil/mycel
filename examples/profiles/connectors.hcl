# REST API Connector (to expose endpoints)
connector "api" {
  type   = "rest"
  driver = "server"
  port   = 3000
}

# Profiled Pricing Connector
# This connector can switch between Magento API, ERP database, or Legacy API
# based on the PRICE_SOURCE environment variable
connector "pricing" {
  type = "profiled"

  # Profile selection via environment variable
  # Set PRICE_SOURCE=magento, PRICE_SOURCE=erp, or PRICE_SOURCE=legacy
  select  = "env('PRICE_SOURCE')"
  default = "magento"

  # Fallback chain: if magento fails, try erp, then legacy
  fallback = ["erp", "legacy"]

  # Magento API profile
  profile "magento" {
    type     = "http"
    driver   = "client"
    base_url = "http://localhost:8080/api"

    auth {
      type  = "bearer"
      token = "magento-api-token"
    }

    # Transform Magento response to normalized format
    transform {
      product_id = "input.entity_id"
      sku        = "input.sku"
      price      = "double(input.price)"
      currency   = "'USD'"
      source     = "'magento'"
    }
  }

  # ERP Database profile
  profile "erp" {
    type     = "database"
    driver   = "sqlite"
    database = "erp.db"

    # Transform ERP data to normalized format
    transform {
      product_id = "string(input.id)"
      sku        = "input.codigo"
      price      = "input.precio"
      currency   = "input.moneda"
      source     = "'erp'"
    }
  }

  # Legacy API profile
  profile "legacy" {
    type     = "http"
    driver   = "client"
    base_url = "http://localhost:9090/legacy"

    # Transform legacy format to normalized format
    transform {
      product_id = "input.prod_id"
      sku        = "input.product_code"
      price      = "double(input.unit_price)"
      currency   = "input.currency_code"
      source     = "'legacy'"
    }
  }
}

# Local SQLite for testing ERP profile
connector "local_db" {
  type     = "database"
  driver   = "sqlite"
  database = "test.db"
}

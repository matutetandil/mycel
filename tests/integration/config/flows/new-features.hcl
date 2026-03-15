# ============================================================
# Flows for testing v1.13.0 features:
# idempotency, async, echo/response block, file upload, headers
# ============================================================

# --- Echo flow (no to block, response block) ---

flow "echo_ping" {
  from {
    connector = "api"
    operation = "GET /echo/ping"
  }

  response {
    status  = "string('ok')"
    service = "string('mycel')"
  }
}

flow "echo_post" {
  from {
    connector = "api"
    operation = "POST /echo/mirror"
  }

  response {
    greeting = "string('Hello, ' + input.name)"
    original = "input.name"
  }
}

# --- Echo flow with status code override ---

flow "echo_created" {
  from {
    connector = "api"
    operation = "POST /echo/created"
  }

  response {
    http_status_code = "201"
    message          = "string('resource created')"
  }
}

flow "echo_not_implemented" {
  from {
    connector = "api"
    operation = "GET /echo/not-implemented"
  }

  response {
    http_status_code = "501"
    error            = "string('not yet implemented')"
  }
}

# --- Request headers accessible in CEL ---

flow "echo_headers" {
  from {
    connector = "api"
    operation = "GET /echo/headers"
  }

  response {
    has_headers = "has(input.headers)"
  }
}

# --- File upload (multipart) ---

flow "upload_file" {
  from {
    connector = "api"
    operation = "POST /echo/upload"
  }

  response {
    has_file = "has(input.document)"
    name     = "input.name"
  }
}

# --- Idempotency (uses memory_cache) ---

flow "idempotent_create" {
  from {
    connector = "api"
    operation = "POST /idempotent/products"
  }

  to {
    connector = "postgres"
    target    = "products"
  }

  idempotency {
    storage = "memory_cache"
    key     = "input.sku"
    ttl     = "1m"
  }

  transform {
    name  = "input.name"
    price = "input.price"
    sku   = "input.sku"
  }
}

# --- Async execution (uses memory_cache) ---

flow "async_create" {
  from {
    connector = "api"
    operation = "POST /async/products"
  }

  to {
    connector = "postgres"
    target    = "products"
  }

  async {
    storage = "memory_cache"
    ttl     = "5m"
  }

  transform {
    name  = "input.name"
    price = "input.price"
    sku   = "input.sku"
  }
}

# --- Headers stripped from DB writes ---

flow "headers_write" {
  from {
    connector = "api"
    operation = "POST /headers/products"
  }

  to {
    connector = "postgres"
    target    = "products"
  }

  transform {
    name  = "input.name"
    price = "input.price"
    sku   = "string('hdr-' + input.name)"
  }
}

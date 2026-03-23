# Aspect: deprecation warnings for v1 endpoints
# Injects HTTP headers and body field into all *_v1 flows
aspect "v1_deprecation" {
  when = "after"
  on   = ["*_v1"]

  response {
    headers = {
      Deprecation    = "true"
      Sunset         = "Thu, 01 Jun 2026 00:00:00 GMT"
      X-API-Version  = "v1"
    }

    _warning = "'This API version is deprecated. Migrate to v2.'"
  }
}

# Aspect: add custom header to all list_* flows
aspect "list_metadata" {
  when = "after"
  on   = ["list_*"]

  response {
    headers = {
      X-Result-Type = "list"
    }
  }
}

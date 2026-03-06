# HTTP client flows (REST -> HTTP client -> mock server)

flow "http_call" {
  from {
    connector = "api"
    operation = "POST /test/http-call"
  }

  transform {
    source  = "'mycel'"
    payload = "input.data"
  }

  to {
    connector = "external_api"
    target    = "/external/data"
    operation = "POST"
  }
}

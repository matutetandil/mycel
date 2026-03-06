service {
  name    = "integration-test"
  version = "1.0.0"

  admin_port = 9090

  rate_limit {
    requests_per_second = 50
    burst               = 200
    key_extractor       = "ip"
  }
}

# HTTP client connector (targets mock server)
connector "external_api" {
  type   = "http"
  driver = "client"

  base_url = env("MOCK_URL", "http://localhost:8888")
  timeout  = "10s"
}

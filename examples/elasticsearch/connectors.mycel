connector "api" {
  type = "rest"
  port = 3000
}

connector "es" {
  type = "elasticsearch"

  nodes    = ["http://localhost:9200"]
  username = env("ES_USER")
  password = env("ES_PASSWORD")
  index    = "products"
  timeout  = "30s"
}

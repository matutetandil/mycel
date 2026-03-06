# Elasticsearch
connector "es" {
  type = "elasticsearch"

  url     = env("ES_NODES", "http://localhost:9200")
  timeout = "30s"
}

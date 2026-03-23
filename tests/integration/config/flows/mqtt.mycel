# MQTT flows for integration tests

# Publish to MQTT via REST
flow "mqtt_publish" {
  from {
    connector = "api"
    operation = "POST /mqtt/publish"
  }
  to {
    connector = "mqtt_pub"
    target    = "test/messages"
  }
}

# Subscribe to MQTT and write to DB
flow "mqtt_consume" {
  from {
    connector = "mqtt_sub"
    operation = "test/messages"
  }
  transform {
    source = "'mqtt'"
    data   = "'received'"
  }
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}

# List MQTT results
flow "mqtt_results" {
  from {
    connector = "api"
    operation = "GET /mqtt/results"
  }
  step "results" {
    connector = "postgres"
    query     = "SELECT * FROM mq_results WHERE source = 'mqtt' ORDER BY id DESC"
  }
  transform {
    output = "step.results"
  }
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}

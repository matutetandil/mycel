# MQTT connector for integration tests
connector "mqtt_pub" {
  type      = "mqtt"
  broker    = env("MQTT_BROKER", "tcp://localhost:1883")
  client_id = "mycel-test-pub"
  qos       = 1
}

connector "mqtt_sub" {
  type      = "mqtt"
  broker    = env("MQTT_BROKER", "tcp://localhost:1883")
  client_id = "mycel-test-sub"
  qos       = 1
}

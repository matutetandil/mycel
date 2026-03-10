service {
  name    = "iot-gateway"
  version = "1.0.0"
}

# MQTT broker for IoT sensor data
connector "sensors" {
  type      = "mqtt"
  broker    = "tcp://localhost:1883"
  client_id = "mycel-iot-gateway"
  qos       = 1
}

# REST API to expose sensor data
connector "api" {
  type = "rest"
  port = 3000
}

# Database for storing readings
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/sensors.db"
}

# Receive sensor readings via MQTT and store them
flow "sensor_reading" {
  from { connector = "sensors", operation = "sensors/+/temperature" }
  transform {
    device_id  = "input._topic"
    value      = "input.temperature"
    unit       = "'celsius'"
    received_at = "now()"
  }
  to { connector = "db", target = "readings" }
}

# Publish alerts when temperature exceeds threshold
flow "temperature_alert" {
  from { connector = "api", operation = "POST /alerts" }
  to   { connector = "sensors", target = "alerts/temperature" }
}

# Configuration for RabbitMQ -> REST integration example

service {
  name        = "rabbit-to-rest-example"
  version     = "1.0.0"
  description = "Integration pattern: Consume from RabbitMQ and call REST APIs"
}

# Environment-specific configuration
environment "development" {
  variables {
    RABBIT_HOST           = "localhost"
    RABBIT_PORT           = "5672"
    RABBIT_USER           = "guest"
    RABBIT_PASS           = "guest"
    FULFILLMENT_API_URL   = "http://localhost:3001"
    FULFILLMENT_API_TOKEN = "dev-token"
    NOTIFICATION_API_URL  = "http://localhost:3002"
    NOTIFICATION_API_KEY  = "dev-api-key"
  }
}

environment "production" {
  variables {
    RABBIT_HOST           = "${RABBIT_HOST}"
    RABBIT_PORT           = "${RABBIT_PORT}"
    RABBIT_USER           = "${RABBIT_USER}"
    RABBIT_PASS           = "${RABBIT_PASS}"
    FULFILLMENT_API_URL   = "${FULFILLMENT_API_URL}"
    FULFILLMENT_API_TOKEN = "${FULFILLMENT_API_TOKEN}"
    NOTIFICATION_API_URL  = "${NOTIFICATION_API_URL}"
    NOTIFICATION_API_KEY  = "${NOTIFICATION_API_KEY}"
  }
}

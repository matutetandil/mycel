# Configuration for RabbitMQ -> REST integration example

service {
  name    = "rabbit-to-rest-example"
  version = "1.0.0"
}

# Environment variables are configured at runtime via:
# - RABBIT_HOST, RABBIT_PORT, RABBIT_USER, RABBIT_PASS
# - FULFILLMENT_API_URL, FULFILLMENT_API_TOKEN
# - NOTIFICATION_API_URL, NOTIFICATION_API_KEY

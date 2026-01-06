# Configuration for REST -> RabbitMQ integration example

service {
  name        = "rest-to-rabbit-example"
  version     = "1.0.0"
  description = "Integration pattern: Receive HTTP requests and publish to RabbitMQ"
}

environment "development" {
  variables {
    API_PORT    = "8080"
    RABBIT_HOST = "localhost"
    RABBIT_PORT = "5672"
    RABBIT_USER = "guest"
    RABBIT_PASS = "guest"
  }
}

environment "production" {
  variables {
    API_PORT    = "${API_PORT}"
    RABBIT_HOST = "${RABBIT_HOST}"
    RABBIT_PORT = "${RABBIT_PORT}"
    RABBIT_USER = "${RABBIT_USER}"
    RABBIT_PASS = "${RABBIT_PASS}"
  }
}

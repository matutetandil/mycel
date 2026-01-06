# Configuration for RabbitMQ -> Exec integration example

service {
  name        = "rabbit-to-exec-example"
  version     = "1.0.0"
  description = "Integration pattern: Consume from RabbitMQ and execute processes/scripts"
}

environment "development" {
  variables {
    RABBIT_HOST        = "localhost"
    RABBIT_PORT        = "5672"
    RABBIT_USER        = "guest"
    RABBIT_PASS        = "guest"
    SCRIPTS_DIR        = "./scripts"
    PYTHON_SCRIPTS_DIR = "./python"
    NODE_ENV           = "development"
  }
}

environment "production" {
  variables {
    RABBIT_HOST        = "${RABBIT_HOST}"
    RABBIT_PORT        = "${RABBIT_PORT}"
    RABBIT_USER        = "${RABBIT_USER}"
    RABBIT_PASS        = "${RABBIT_PASS}"
    SCRIPTS_DIR        = "/app/scripts"
    PYTHON_SCRIPTS_DIR = "/app/python"
    NODE_ENV           = "production"
  }
}

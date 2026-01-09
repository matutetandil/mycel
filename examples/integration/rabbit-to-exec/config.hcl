# Configuration for RabbitMQ -> Exec integration example

service {
  name    = "rabbit-to-exec-example"
  version = "1.0.0"
}

# Environment variables are configured at runtime via:
# - RABBIT_HOST, RABBIT_PORT, RABBIT_USER, RABBIT_PASS
# - SCRIPTS_DIR, PYTHON_SCRIPTS_DIR

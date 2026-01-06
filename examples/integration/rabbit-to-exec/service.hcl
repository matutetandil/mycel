# Integration Pattern: RabbitMQ -> Exec
#
# Use case: Consume messages from a queue and execute local processes/scripts
# Common scenarios:
#   - Process files (PDF generation, image processing, video transcoding)
#   - Run data processing scripts (Python, R, shell)
#   - Execute legacy system integrations
#   - Trigger batch jobs

connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"

  host     = env("RABBIT_HOST", "localhost")
  port     = env("RABBIT_PORT", 5672)
  username = env("RABBIT_USER", "guest")
  password = env("RABBIT_PASS", "guest")
  vhost    = "/"

  prefetch = 5  # Lower prefetch for heavy processing tasks

  reconnect {
    enabled      = true
    interval     = "5s"
    max_attempts = 0
  }
}

connector "exec" {
  type = "exec"

  working_dir = env("SCRIPTS_DIR", "/app/scripts")
  timeout     = "5m"
  shell       = "/bin/bash"

  env {
    PATH       = "/usr/local/bin:/usr/bin:/bin"
    PYTHONPATH = "/app/lib"
    NODE_ENV   = env("NODE_ENV", "production")
  }
}

connector "exec_python" {
  type = "exec"

  working_dir = env("PYTHON_SCRIPTS_DIR", "/app/python")
  timeout     = "10m"
  shell       = "/bin/bash"

  env {
    PATH              = "/usr/local/bin:/usr/bin:/bin"
    PYTHONPATH        = "/app/python/lib"
    VIRTUAL_ENV       = "/app/python/venv"
    PYTHONUNBUFFERED  = "1"
  }
}

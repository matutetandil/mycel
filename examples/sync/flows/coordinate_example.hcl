# Coordinate Example - Parent/Child Entity Processing
# Ensures child entities wait for their parent to be processed first

flow "process_entity" {
  from {
    connector = "rabbitmq"
    operation = "queue:entities"
  }

  coordinate {
    storage              = "redis"
    timeout              = "60s"
    on_timeout           = "retry"
    max_retries          = 3
    max_concurrent_waits = 10
  }

  transform {
    id        = "input.body.id"
    type      = "input.headers.type"
    parent_id = "input.headers.parent_id ?? ''"
    name      = "input.body.name"
    data      = "input.body"
  }

  to {
    connector = "postgres"
    operation = "entities"
  }
}

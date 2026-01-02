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
    on_timeout           = "retry"  # Options: fail, retry, skip, pass
    max_retries          = 3
    max_concurrent_waits = 10

    # Child entities wait for their parent
    wait {
      when = "input.headers.type == 'child'"
      for  = "'entity:' + input.headers.parent_id + ':ready'"
    }

    # Parent entities signal when processed
    signal {
      when = "input.headers.type == 'parent'"
      emit = "'entity:' + input.body.id + ':ready'"
      ttl  = "5m"
    }

    # Skip waiting if parent already exists in database
    preflight {
      connector = "postgres"
      query     = "SELECT 1 FROM entities WHERE id = :parent_id"
      params    = { parent_id = "input.headers.parent_id" }
      if_exists = "pass"
    }
  }

  transform {
    output.id        = "input.body.id"
    output.type      = "input.headers.type"
    output.parent_id = "input.headers.parent_id ?? ''"
    output.name      = "input.body.name"
    output.data      = "input.body"
  }

  to {
    connector = "postgres"
    target    = "entities"
  }
}

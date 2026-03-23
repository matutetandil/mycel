# Fan-out integration tests: multiple flows sharing the same from connector+operation

# ============================================================================
# REST Fan-Out: Two flows share POST /fanout/create
# The first registered flow returns the HTTP response.
# The second runs concurrently as fire-and-forget.
# ============================================================================

flow "fanout_primary" {
  from {
    connector = "api"
    operation = "POST /fanout/create"
  }
  transform {
    name   = "input.name"
    target = "'primary'"
  }
  to {
    connector = "postgres"
    target    = "fanout_primary"
  }
}

flow "fanout_secondary" {
  from {
    connector = "api"
    operation = "POST /fanout/create"
  }
  transform {
    name   = "input.name"
    target = "'secondary'"
  }
  to {
    connector = "postgres"
    target    = "fanout_secondary"
  }
}

# Read helpers for verifying fan-out results
flow "fanout_read_primary" {
  from {
    connector = "api"
    operation = "GET /fanout/primary"
  }
  to {
    connector = "postgres"
    target    = "fanout_primary"
  }
}

flow "fanout_read_secondary" {
  from {
    connector = "api"
    operation = "GET /fanout/secondary"
  }
  to {
    connector = "postgres"
    target    = "fanout_secondary"
  }
}

# ============================================================================
# MQ Fan-Out: Two consumer flows share the same RabbitMQ queue
# Both flows execute concurrently for each message.
# Message is ACKed only after both complete.
# ============================================================================

# Publish to the fan-out queue via REST
flow "fanout_mq_publish" {
  from {
    connector = "api"
    operation = "POST /fanout/mq/publish"
  }
  to {
    connector = "rabbit_fanout_pub"
    target    = "fanout_queue"
  }
}

# Consumer A: writes with source='flow_a'
flow "fanout_mq_consumer_a" {
  from {
    connector = "rabbit_fanout_sub"
    operation = "fanout_queue"
  }
  transform {
    source = "'flow_a'"
    data   = "'consumed'"
  }
  to {
    connector = "postgres"
    target    = "fanout_mq_results"
  }
}

# Consumer B: writes with source='flow_b'
flow "fanout_mq_consumer_b" {
  from {
    connector = "rabbit_fanout_sub"
    operation = "fanout_queue"
  }
  transform {
    source = "'flow_b'"
    data   = "'consumed'"
  }
  to {
    connector = "postgres"
    target    = "fanout_mq_results"
  }
}

# Read MQ fan-out results
flow "fanout_mq_read_results" {
  from {
    connector = "api"
    operation = "GET /fanout/mq/results"
  }
  step "results" {
    connector = "postgres"
    query     = "SELECT * FROM fanout_mq_results ORDER BY id DESC"
  }
  transform {
    output = "step.results"
  }
  to {
    connector = "postgres"
    target    = "fanout_mq_results"
  }
}

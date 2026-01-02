# Sync Example

This example demonstrates the synchronization primitives in Mycel:

- **Lock (Mutex)**: Exclusive access to a resource by key
- **Semaphore**: Limit concurrent access to a resource
- **Coordinate**: Signal/Wait pattern for dependency coordination
- **Flow Triggers**: Cron/interval scheduling

## Use Cases

### 1. Lock - User Payment Processing

Ensure only one payment is processed per user at a time:

```hcl
flow "process_payment" {
  from { connector = "rabbitmq", operation = "queue:payments" }

  lock {
    storage = "redis"
    key     = "'user:' + input.body.user_id"
    timeout = "30s"
    wait    = true
    retry   = "100ms"
  }

  to { connector = "postgres", target = "payments" }
}
```

### 2. Semaphore - External API Rate Limiting

Limit concurrent requests to an external API:

```hcl
flow "call_external_api" {
  from { connector = "rabbitmq", operation = "queue:requests" }

  semaphore {
    storage     = "redis"
    key         = "'external_api'"
    max_permits = 10
    timeout     = "30s"
    lease       = "60s"
  }

  to { connector = "external_api", target = "POST /process" }
}
```

### 3. Coordinate - Parent/Child Entity Processing

Ensure child entities wait for their parent to be processed first:

```hcl
flow "process_entity" {
  from { connector = "rabbitmq", operation = "queue:entities" }

  coordinate {
    storage              = "redis"
    timeout              = "60s"
    on_timeout           = "retry"
    max_retries          = 3
    max_concurrent_waits = 10

    wait {
      when = "input.headers.type == 'child'"
      for  = "'entity:' + input.headers.parent_id + ':ready'"
    }

    signal {
      when = "input.headers.type == 'parent'"
      emit = "'entity:' + input.body.id + ':ready'"
      ttl  = "5m"
    }

    preflight {
      connector = "postgres"
      query     = "SELECT 1 FROM entities WHERE id = :parent_id"
      params    = { parent_id = "input.headers.parent_id" }
      if_exists = "pass"
    }
  }

  to { connector = "postgres", target = "entities" }
}
```

### 4. Flow Triggers - Scheduled Jobs

Run jobs on a schedule:

```hcl
# Daily cleanup at 3 AM
flow "daily_cleanup" {
  when = "0 3 * * *"

  lock {
    storage = "redis"
    key     = "'job:daily_cleanup'"
    timeout = "1h"
    wait    = false
  }

  to {
    connector = "postgres"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

# Health check every 5 minutes
flow "health_ping" {
  when = "@every 5m"

  to { connector = "monitoring", target = "POST /ping" }
}
```

## Running the Example

```bash
# Start dependencies
docker-compose up -d

# Start Mycel
mycel start --config ./examples/sync
```

## Configuration

See the HCL files in this directory:

- `config.hcl` - Main configuration
- `connectors.hcl` - Connector definitions
- `flows/` - Flow definitions

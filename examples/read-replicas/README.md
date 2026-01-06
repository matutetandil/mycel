# Database Read Replicas Example

This example demonstrates automatic read/write routing with database replicas in Mycel.

## How It Works

Mycel automatically routes queries based on operation type:

| Operation | Destination |
|-----------|-------------|
| SELECT | Replica (load balanced) |
| INSERT | Primary |
| UPDATE | Primary |
| DELETE | Primary |

## Configuration

### PostgreSQL

```hcl
connector "postgres" {
  type   = "database"
  driver = "postgres"

  # Primary (for writes)
  host     = "pg-primary"
  port     = 5432
  user     = "mycel"
  password = "secret"
  database = "app"

  # Read replicas
  replicas {
    hosts = [
      "pg-replica-1:5432",
      "pg-replica-2:5432",
    ]

    strategy = "round_robin"
    max_lag  = "1s"
  }
}
```

### MySQL

```hcl
connector "mysql" {
  type   = "database"
  driver = "mysql"

  host     = "mysql-primary"
  port     = 3306
  user     = "mycel"
  password = "secret"
  database = "app"

  replicas {
    hosts = [
      "mysql-replica-1:3306",
      "mysql-replica-2:3306",
    ]

    strategy = "random"
  }
}
```

## Load Balancing Strategies

| Strategy | Description |
|----------|-------------|
| `round_robin` | Distribute evenly across replicas |
| `random` | Random replica selection |
| `least_conn` | Route to replica with fewest connections |

## Replication Lag Handling

```hcl
replicas {
  hosts    = ["replica-1:5432", "replica-2:5432"]
  max_lag  = "1s"  # Skip replicas with lag > 1s
}
```

When `max_lag` is set, Mycel:
1. Monitors replication lag on each replica
2. Excludes replicas exceeding the threshold
3. Falls back to primary if all replicas are lagging

## Read-After-Write Consistency

After a write, replicas may not have the latest data. Force primary reads when needed:

```hcl
flow "create_and_fetch" {
  # Write to primary
  to {
    connector.postgres = "INSERT INTO users ..."
  }

  # Force read from primary
  transform {
    force_primary = "true"
  }

  to {
    connector.postgres = "SELECT * FROM users WHERE id = :id"
  }
}
```

## Using Connector Profiles

For explicit control over read vs write routing:

```hcl
connector "database" {
  select  = "operation_type"  # From flow context
  default = "read"

  profile "read" {
    type   = "database"
    driver = "postgres"
    host   = "replica-1"
    # ...
  }

  profile "write" {
    type   = "database"
    driver = "postgres"
    host   = "primary"
    # ...
  }
}
```

## Docker Compose Setup

### PostgreSQL with Streaming Replication

```yaml
services:
  pg-primary:
    image: postgres:15
    environment:
      POSTGRES_USER: mycel
      POSTGRES_PASSWORD: secret
    command: |
      postgres
      -c wal_level=replica
      -c max_wal_senders=3
      -c max_replication_slots=3

  pg-replica-1:
    image: postgres:15
    environment:
      POSTGRES_USER: mycel
      POSTGRES_PASSWORD: secret
      POSTGRES_MASTER_HOST: pg-primary
    command: |
      postgres
      -c primary_conninfo='host=pg-primary user=replicator password=secret'
      -c hot_standby=on

  pg-replica-2:
    image: postgres:15
    depends_on:
      - pg-primary
    # Similar to replica-1
```

### MySQL with Replication

```yaml
services:
  mysql-primary:
    image: mysql:8
    environment:
      MYSQL_ROOT_PASSWORD: secret
      MYSQL_DATABASE: app
    command: |
      --server-id=1
      --log-bin=mysql-bin
      --gtid-mode=ON
      --enforce-gtid-consistency=ON

  mysql-replica:
    image: mysql:8
    environment:
      MYSQL_ROOT_PASSWORD: secret
    command: |
      --server-id=2
      --read-only=ON
      --gtid-mode=ON
      --enforce-gtid-consistency=ON
```

## Running

```bash
# Start databases with replicas
docker-compose up -d

# Set environment
export PG_PRIMARY_HOST=localhost
export PG_REPLICA_1=localhost:5433
export PG_REPLICA_2=localhost:5434

# Start Mycel
mycel start --config ./examples/read-replicas

# Test read (goes to replica)
curl http://localhost:8080/users

# Test write (goes to primary)
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name": "John", "email": "john@example.com"}'
```

## Monitoring

```bash
# Check which server handled the query
curl http://localhost:8080/metrics | grep database

# mycel_database_queries_total{server="primary"} 42
# mycel_database_queries_total{server="replica-1"} 156
# mycel_database_queries_total{server="replica-2"} 148
```

## Best Practices

1. **Set appropriate max_lag**: Too low causes frequent primary reads, too high risks stale data
2. **Use force_primary sparingly**: Only when read-after-write consistency is required
3. **Monitor replication lag**: Alert when lag exceeds threshold
4. **Size replica pool appropriately**: More replicas = better read scaling
5. **Consider geographic distribution**: Place replicas near users for lower latency

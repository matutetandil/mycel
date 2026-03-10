# Troubleshooting Guide

Common issues and how to solve them.

## Quick Diagnosis

```bash
# Validate your configuration
mycel validate --config ./my-service

# Check connector connectivity
mycel check --config ./my-service

# Run with debug logging
mycel start --config ./my-service --log-level=debug
```

---

## Startup Issues

### "Port already in use"

**Error:**
```
ERROR  Failed to start REST server: listen tcp :3000: bind: address already in use
```

**Cause:** Another process is using the port.

**Solution:**

```bash
# Find what's using the port
lsof -i :3000

# Kill it (replace PID with actual process ID)
kill -9 <PID>

# Or use a different port in service.hcl
service {
  port = 3001
}
```

---

### "Failed to parse configuration"

**Error:**
```
ERROR  Failed to parse configuration: ./connectors.hcl:15,3-7: Invalid block...
```

**Cause:** Syntax error in your HCL file.

**Solution:**

1. Check the line number mentioned (line 15 in this case)
2. Common issues:
   - Missing closing brace `}`
   - Missing `=` in attribute assignment
   - Using `"` instead of `=` for attributes
   - Typo in block name

**Example of wrong vs correct:**

```hcl
# WRONG - missing =
connector "db" {
  type "database"    # Missing =
}

# CORRECT
connector "db" {
  type = "database"
}
```

```hcl
# WRONG - wrong brace
connector "db" {
  type = "database"

# CORRECT - close the brace
connector "db" {
  type = "database"
}
```

---

### "Unknown connector type"

**Error:**
```
ERROR  Unknown connector type: postgresql
```

**Cause:** The connector type is misspelled or not supported.

**Solution:**

Use the correct type name:

| You wrote | Correct |
|-----------|---------|
| `postgresql` | `database` with `driver = "postgres"` |
| `mysql` | `database` with `driver = "mysql"` |
| `mongo` | `database` with `driver = "mongodb"` |
| `rabbit` | `rabbitmq` |
| `redis` | `cache` with `driver = "redis"` |

**Example:**
```hcl
# WRONG
connector "db" {
  type = "postgresql"
}

# CORRECT
connector "db" {
  type   = "database"
  driver = "postgres"
  host   = "localhost"
}
```

---

## Database Issues

### "Connection refused"

**Error:**
```
ERROR  Failed to connect to database: dial tcp 127.0.0.1:5432: connect: connection refused
```

**Cause:** Database server is not running or wrong host/port.

**Solution:**

1. Check if the database is running:
   ```bash
   # PostgreSQL
   pg_isready -h localhost -p 5432

   # MySQL
   mysqladmin ping -h localhost

   # MongoDB
   mongosh --eval "db.runCommand({ping:1})"
   ```

2. Verify host and port in your config:
   ```hcl
   connector "db" {
     type   = "database"
     driver = "postgres"
     host   = "localhost"  # Is this correct?
     port   = 5432         # Is this correct?
   }
   ```

3. If using Docker, ensure containers are on the same network:
   ```yaml
   # docker-compose.yml
   services:
     mycel:
       depends_on:
         - postgres
     postgres:
       # ...
   ```

---

### "Authentication failed"

**Error:**
```
ERROR  Failed to connect: password authentication failed for user "mycel"
```

**Cause:** Wrong username or password.

**Solution:**

1. Verify credentials:
   ```hcl
   connector "db" {
     user     = env("DB_USER", "postgres")
     password = env("DB_PASSWORD", "")
   }
   ```

2. Check environment variables are set:
   ```bash
   export DB_USER=mycel
   export DB_PASSWORD=secret
   mycel start
   ```

3. For Docker:
   ```bash
   docker run -e DB_USER=mycel -e DB_PASSWORD=secret ...
   ```

---

### "Database does not exist"

**Error:**
```
ERROR  database "myapp" does not exist
```

**Solution:**

Create the database first:

```bash
# PostgreSQL
createdb myapp

# Or via psql
psql -c "CREATE DATABASE myapp;"

# MySQL
mysql -e "CREATE DATABASE myapp;"
```

---

## Flow Issues

### "Flow not being triggered"

**Symptom:** You make a request but nothing happens.

**Diagnosis:**

1. Check if the path matches exactly:
   ```hcl
   flow "get_users" {
     from {
       connector = "api"
       path      = "GET /users"  # Must match exactly
     }
   }
   ```

   ```bash
   # This works
   curl http://localhost:3000/users

   # This does NOT work (trailing slash)
   curl http://localhost:3000/users/
   ```

2. Check method matches:
   ```bash
   # Your flow expects POST
   path = "POST /users"

   # But you're sending GET
   curl http://localhost:3000/users  # Wrong!
   curl -X POST http://localhost:3000/users  # Correct
   ```

3. Enable debug logging:
   ```bash
   mycel start --log-level=debug
   ```
   Look for: `"Matched flow: get_users"` or `"No flow matched for: GET /users"`

---

### "Transform expression error"

**Error:**
```
ERROR  Transform failed: no such key: user_name
```

**Cause:** Accessing a field that doesn't exist in the input.

**Solution:**

1. Use the null coalescing operator `??` for optional fields:
   ```hcl
   transform {
     name = "input.user_name ?? input.name ?? 'Unknown'"
   }
   ```

2. Check what's actually in `input`:
   ```hcl
   # Temporarily log the input
   transform {
     _debug = "string(input)"  # Will show in response
     name   = "input.name"
   }
   ```

3. Common input structures:
   - REST body: `input.field_name`
   - URL params: `input.id` (from `/users/:id`)
   - Query params: `input.query.limit`
   - Headers: `input.headers.authorization`

---

### "Type validation failed"

**Error:**
```
{
  "error": "validation failed",
  "details": {
    "email": "invalid format: must be a valid email"
  }
}
```

**Cause:** Input doesn't match your type definition.

**Solution:**

1. Check your type definition:
   ```hcl
   type "user_input" {
     email = string { required = true, format = "email" }
   }
   ```

2. Ensure client sends correct data:
   ```bash
   # Wrong
   curl -X POST ... -d '{"email": "not-an-email"}'

   # Correct
   curl -X POST ... -d '{"email": "user@example.com"}'
   ```

3. Make fields optional if needed:
   ```hcl
   email = string { required = false, format = "email" }
   ```

---

## Message Queue Issues

### "RabbitMQ connection failed"

**Error:**
```
ERROR  Failed to connect to RabbitMQ: dial tcp: connection refused
```

**Solution:**

1. Check RabbitMQ is running:
   ```bash
   # Check status
   rabbitmqctl status

   # Or with Docker
   docker ps | grep rabbitmq
   ```

2. Verify credentials:
   ```hcl
   connector "mq" {
     type     = "rabbitmq"
     host     = "localhost"
     port     = 5672
     user     = "guest"      # Default
     password = "guest"      # Default
     vhost    = "/"          # Default
   }
   ```

3. Check if management plugin shows the connection:
   Open http://localhost:15672 (guest/guest)

---

### "Queue not found"

**Error:**
```
ERROR  Queue 'orders' not found
```

**Cause:** The queue doesn't exist and auto-creation is disabled.

**Solution:**

1. Create the queue manually via RabbitMQ management UI

2. Or let Mycel create it by adding `auto_create`:
   ```hcl
   flow "process_orders" {
     from {
       connector   = "mq"
       queue       = "orders"
       auto_create = true
     }
   }
   ```

---

## Performance Issues

### "Requests are slow"

**Diagnosis:**

1. Enable metrics:
   ```bash
   curl http://localhost:3000/metrics
   ```

2. Check these metrics:
   - `mycel_flow_duration_seconds` - Time per flow
   - `mycel_connector_duration_seconds` - Time per connector call
   - `mycel_db_pool_waiting` - Database pool exhaustion

**Common causes and solutions:**

| Symptom | Cause | Solution |
|---------|-------|----------|
| High `db_duration` | Slow queries | Add indexes, optimize queries |
| High `pool_waiting` | Not enough connections | Increase `pool_size` |
| High `flow_duration` | Complex transforms | Simplify CEL expressions |
| Spiky latency | No caching | Add cache layer |

**Add caching:**
```hcl
flow "get_product" {
  cache {
    storage = "cache"
    ttl     = "5m"
    key     = "'product:' + input.id"
  }
  from { ... }
  to { ... }
}
```

---

### "Memory usage keeps growing"

**Cause:** Usually unbounded caches or connection leaks.

**Solution:**

1. Set cache limits:
   ```hcl
   connector "cache" {
     type     = "cache"
     driver   = "memory"
     max_size = 10000  # Max entries
   }
   ```

2. Set connection pool limits:
   ```hcl
   connector "db" {
     pool_size     = 10
     max_idle_time = "5m"
   }
   ```

---

## Docker/Kubernetes Issues

### "Config not found in container"

**Error:**
```
ERROR  No configuration found at /etc/mycel
```

**Solution:**

Ensure volume is mounted correctly:

```bash
# Wrong - mounting file instead of directory
docker run -v ./service.hcl:/etc/mycel ...

# Correct - mount the directory
docker run -v ./my-config:/etc/mycel ...
```

For Kubernetes, check your ConfigMap is mounted:
```yaml
volumeMounts:
  - name: config
    mountPath: /etc/mycel
volumes:
  - name: config
    configMap:
      name: mycel-config
```

---

### "Permission denied"

**Error:**
```
ERROR  Failed to read config: open /etc/mycel/service.hcl: permission denied
```

**Cause:** Container runs as non-root but files have wrong permissions.

**Solution:**

```bash
# Fix permissions on host
chmod -R 755 ./my-config

# Or run container as root (not recommended for production)
docker run --user root ...
```

---

## Still Stuck?

1. **Check logs with debug level:**
   ```bash
   mycel start --log-level=debug --log-format=text
   ```

2. **Validate configuration:**
   ```bash
   mycel validate --config ./my-service
   ```

3. **Test connectivity:**
   ```bash
   mycel check --config ./my-service
   ```

4. **Search existing issues:**
   https://github.com/matutetandil/mycel/issues

5. **Open a new issue** with:
   - Mycel version (`mycel version`)
   - Your configuration (sanitized)
   - Full error message
   - Steps to reproduce

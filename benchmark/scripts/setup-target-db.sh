#!/bin/bash
# <UDF name="database_ip" label="PostgreSQL IP" default="127.0.0.1" />
# <UDF name="mycel_image" label="Mycel image" default="ghcr.io/matutetandil/mycel:latest" />
# StackScript to setup Mycel benchmark target (with PostgreSQL database)
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Linode injects UDF values as $LINODE_<NAME> (uppercase)
MYCEL_IMAGE="${LINODE_MYCEL_IMAGE:-${mycel_image:-${MYCEL_IMAGE:-ghcr.io/matutetandil/mycel:latest}}}"
echo "Mycel image: $MYCEL_IMAGE"

DB_IP="${LINODE_DATABASE_IP:-${database_ip:-${DATABASE_IP:-127.0.0.1}}}"
echo "Database IP: $DB_IP"

# Install Docker
curl -fsSL https://get.docker.com | sh

# Create Mycel config directory
mkdir -p /opt/mycel/config

# Save DB IP for reference
echo "$DB_IP" > /opt/mycel/database_ip

# ---------------------------------------------------------------------------
# Mycel configuration
# ---------------------------------------------------------------------------

# Service config
cat > /opt/mycel/config/config.mycel << 'HCL'
service {
  name    = "benchmark"
  version = "1.0.0"

  rate_limit {
    enabled = false
  }
}
HCL

# Connectors: REST API + PostgreSQL (remote VPS)
cat > /opt/mycel/config/connectors.mycel << HCL
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = "${DB_IP}"
  port     = 5432
  database = "bench"
  user     = "bench"
  password = "bench"
  ssl_mode = "disable"
}
HCL

# Flows: transform-only + CRUD with PostgreSQL
cat > /opt/mycel/config/flows.mycel << 'HCL'
# Echo with transforms -- measures Mycel overhead: HTTP parse + CEL eval + JSON serialize
flow "process" {
  from {
    connector = "api"
    operation = "POST /process"
  }
  response {
    id         = "uuid()"
    email      = "lower(input.email)"
    name       = "upper(input.name)"
    created_at = "now()"
    tag        = "input.name + '-' + input.email"
  }
}

# Simple echo -- baseline: just HTTP round-trip, no transforms
flow "echo" {
  from {
    connector = "api"
    operation = "POST /echo"
  }
}

# Health -- GET with zero processing
flow "ping" {
  from {
    connector = "api"
    operation = "GET /ping"
  }
}

# Heavy transforms -- 12 CEL expressions per request
flow "heavy" {
  from {
    connector = "api"
    operation = "POST /heavy"
  }
  response {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    name       = "upper(trim(input.name))"
    domain     = "input.email.contains('@') ? split(input.email, '@')[1] : 'unknown'"
    hash       = "hash_sha256(input.email)"
    created_at = "now()"
    unix_ts    = "now_unix()"
    tag        = "lower(input.name) + '-' + string(now_unix())"
    is_gmail   = "input.email.endsWith('@gmail.com')"
    name_len   = "len(input.name)"
    greeting   = "'Hello, ' + upper(input.name) + '! Welcome.'"
    slug       = "lower(replace(trim(input.name), ' ', '-'))"
  }
}

# Array processing -- filter, sort, aggregate
flow "array" {
  from {
    connector = "api"
    operation = "POST /array"
  }
  response {
    count     = "input.items.size()"
    total     = "sum(pluck(input.items, 'price'))"
    average   = "avg(pluck(input.items, 'price'))"
    max_price = "max_val(pluck(input.items, 'price'))"
    min_price = "min_val(pluck(input.items, 'price'))"
    sorted    = "sort_by(input.items, 'price')"
    names     = "pluck(input.items, 'name')"
    expensive = "input.items.filter(x, x.price > 50)"
  }
}

# CRUD -- PostgreSQL database operations (realistic benchmark)
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    id         = "uuid()"
    email      = "lower(input.email)"
    name       = "input.name"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "users"
  }
}

# A read has to stay a read. `target = "users"` is SELECT * with no bound, so
# every request shipped the whole table — and the write phases keep growing it.
# Measured locally at 7,500 rows: 1 MB per response, against 14 KB for a page.
# That is what the read phase was timing, and it is why the tail latencies in
# the old results climb the longer the run goes.
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "db"
    query     = "SELECT id, email, name, created_at FROM users ORDER BY created_at DESC LIMIT 100"
  }
}
HCL

# ---------------------------------------------------------------------------
# Wait for PostgreSQL to be reachable
# ---------------------------------------------------------------------------

echo "Waiting for PostgreSQL at ${DB_IP}:5432..."
apt-get install -y postgresql-client > /dev/null 2>&1 || true
for i in $(seq 1 60); do
  if pg_isready -h "$DB_IP" -p 5432 -U bench > /dev/null 2>&1; then
    echo "PostgreSQL is reachable!"
    break
  fi
  printf "."
  sleep 3
done
echo ""

# Verify the HCL config has the correct IP
echo "Connector config:"
grep host /opt/mycel/config/connectors.mycel

# ---------------------------------------------------------------------------
# Mycel - full resources (no DB on this VPS)
# ---------------------------------------------------------------------------

docker pull "$MYCEL_IMAGE"

docker run -d \
  --name mycel \
  --restart unless-stopped \
  -p 3000:3000 \
  -p 9090:9090 \
  -v /opt/mycel/config:/etc/mycel \
  -e MYCEL_ENV=production \
  -e MYCEL_LOG_LEVEL=warn \
  -e MYCEL_LOG_FORMAT=json \
  --memory=900m \
  --cpus=1 \
  "$MYCEL_IMAGE"

# Wait for Mycel to be ready
echo "Waiting for Mycel to start..."
for i in $(seq 1 60); do
  if curl -sf http://localhost:3000/health > /dev/null 2>&1; then
    echo "Mycel is ready!"
    docker exec mycel mycel version > /opt/mycel/version.txt 2>&1 || true
    docker image inspect --format '{{index .RepoDigests 0}}' "$MYCEL_IMAGE" >> /opt/mycel/version.txt 2>&1 || true
    cat /opt/mycel/version.txt
    exit 0
  fi
  sleep 3
done
echo "ERROR: Mycel did not become healthy in 3 minutes. Logs:"
docker logs --tail 50 mycel 2>&1 || true
exit 1

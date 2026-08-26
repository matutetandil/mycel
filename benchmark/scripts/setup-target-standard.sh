#!/bin/bash
# <UDF name="mycel_image" label="Mycel image" default="ghcr.io/matutetandil/mycel:latest" />
# StackScript to setup Mycel benchmark target (standard -- no database)
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Linode injects UDF values as $LINODE_<NAME> (uppercase)
MYCEL_IMAGE="${LINODE_MYCEL_IMAGE:-${mycel_image:-${MYCEL_IMAGE:-ghcr.io/matutetandil/mycel:latest}}}"
echo "Mycel image: $MYCEL_IMAGE"

# Install Docker
curl -fsSL https://get.docker.com | sh

# Create Mycel config directory
mkdir -p /opt/mycel/config

# ---------------------------------------------------------------------------
# Mycel configuration (standard benchmark -- no DB connector)
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

# Connector: REST API only (no database)
cat > /opt/mycel/config/connectors.mycel << 'HCL'
connector "api" {
  type = "rest"
  port = 3000
}
HCL

# Flows: transform-only endpoints (no CRUD, no database)
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
HCL

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

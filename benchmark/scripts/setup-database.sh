#!/bin/bash
# <UDF name="dummy" label="dummy" default="1" />
# StackScript to setup PostgreSQL for Mycel benchmark
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Install Docker
curl -fsSL https://get.docker.com | sh

# Run PostgreSQL - tuned for benchmark on 1GB VPS
docker run -d \
  --name postgres \
  --restart unless-stopped \
  -p 5432:5432 \
  -e POSTGRES_USER=bench \
  -e POSTGRES_PASSWORD=bench \
  -e POSTGRES_DB=bench \
  --memory=800m \
  postgres:16-alpine \
  -c shared_buffers=256MB \
  -c effective_cache_size=512MB \
  -c max_connections=200 \
  -c work_mem=4MB \
  -c maintenance_work_mem=64MB \
  -c checkpoint_completion_target=0.9 \
  -c wal_buffers=16MB \
  -c random_page_cost=1.1 \
  -c listen_addresses='*'

# Wait for Postgres to be ready
echo "Waiting for PostgreSQL..."
for i in $(seq 1 30); do
  if docker exec postgres pg_isready -U bench > /dev/null 2>&1; then
    echo "PostgreSQL is ready!"
    break
  fi
  sleep 2
done

# Allow connections from any IP (benchmark VPS network)
docker exec postgres bash -c "echo 'host all all 0.0.0.0/0 md5' >> /var/lib/postgresql/data/pg_hba.conf"
docker exec postgres pg_ctl reload -D /var/lib/postgresql/data

# Create schema (retry - pg_isready can return true before DDL is possible)
for i in $(seq 1 10); do
  if docker exec postgres psql -U bench -d bench -c "
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY,
      email TEXT NOT NULL,
      name TEXT NOT NULL,
      created_at TIMESTAMPTZ DEFAULT NOW()
    );
    CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
  " 2>/dev/null; then
    echo "Schema created!"
    break
  fi
  echo "Schema creation attempt $i failed, retrying..."
  sleep 2
done

# Verify table exists
docker exec postgres psql -U bench -d bench -c "\dt users" | grep -q users || echo "WARNING: users table not created!"

echo "PostgreSQL setup complete!"

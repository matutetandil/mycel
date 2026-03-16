#!/bin/bash
# Test: Cache (Redis + Memory) as middleware on database flows
source "$(dirname "$0")/lib.sh"

COMPOSE_FILE="$(dirname "$0")/../docker-compose.yml"

echo "=== Cache ==="

# Create a user first so there's data to cache
http_body POST "$BASE/pg/users" '{"name":"CacheUser","email":"cache@test.com"}' > /dev/null

# Flush any stale redis cache so the next GET reads fresh from postgres.
# Other parallel tests may have triggered a cache population before CacheUser existed.
docker compose -f "$COMPOSE_FILE" exec -T redis redis-cli DEL cached_users > /dev/null 2>&1

# Redis cached GET (cache miss — reads from postgres, which now has CacheUser)
status=$(http_status GET "$BASE/cache/redis/users")
assert_status "Redis cached GET returns 200" "200" "$status"

body=$(http_body GET "$BASE/cache/redis/users")
assert_contains "Redis cached response has data" "CacheUser|cache@test.com" "$body"

# Memory cached GET
status=$(http_status GET "$BASE/cache/memory/users")
assert_status "Memory cached GET returns 200" "200" "$status"

body=$(http_body GET "$BASE/cache/memory/users")
assert_contains "Memory cached response has data" "CacheUser|cache@test.com" "$body"

report

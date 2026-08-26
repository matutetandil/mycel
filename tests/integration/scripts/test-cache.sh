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

# The same, through Sentinel: the connector asks which server is master and
# caches into that one, rather than being handed an address. Without a Sentinel
# to ask, the block could be parsed into a client nothing ever used.
docker compose -f "$COMPOSE_FILE" exec -T redis redis-cli DEL cached_users_sentinel > /dev/null 2>&1

status=$(http_status GET "$BASE/cache/sentinel/users")
assert_status "Sentinel-backed cached GET returns 200" "200" "$status"

body=$(http_body GET "$BASE/cache/sentinel/users")
assert_contains "Sentinel-backed response has data" "CacheUser|cache@test.com" "$body"

# And the key is in the server Sentinel named, which is what proves the lookup
# happened rather than a default address being used.
cached=$(docker compose -f "$COMPOSE_FILE" exec -T redis redis-cli EXISTS cached_users_sentinel 2>/dev/null | tr -d '\r')
assert_contains "The value was written to the master Sentinel named" "1" "$cached"

# And across a cluster, where the key belongs to whichever node owns its slot.
status=$(http_status GET "$BASE/cache/cluster/users")
assert_status "Cluster-backed cached GET returns 200" "200" "$status"

body=$(http_body GET "$BASE/cache/cluster/users")
assert_contains "Cluster-backed response has data" "CacheUser|cache@test.com" "$body"

# The value is on one of the three, not on all of them: a cluster shards.
found=$(for node in redis-cluster-1 redis-cluster-2 redis-cluster-3; do
  docker compose -f "$COMPOSE_FILE" exec -T "$node" redis-cli EXISTS cached_users_cluster 2>/dev/null | tr -d '\r'
done | grep -c '^1$')
assert_contains "The value landed on the node that owns its slot" "1" "$found"

# Memory cached GET
status=$(http_status GET "$BASE/cache/memory/users")
assert_status "Memory cached GET returns 200" "200" "$status"

body=$(http_body GET "$BASE/cache/memory/users")
assert_contains "Memory cached response has data" "CacheUser|cache@test.com" "$body"

report

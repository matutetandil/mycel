# Redis Cluster and Sentinel Example

This example demonstrates high-availability Redis configurations in Mycel.

## Redis Cluster Mode

For horizontal scaling with automatic sharding across nodes.

```hcl
connector "redis_cluster" {
  type   = "cache"
  driver = "redis"

  cluster {
    nodes = [
      "redis-node-1:6379",
      "redis-node-2:6379",
      "redis-node-3:6379",
    ]

    read_only        = true   # Read from replicas
    route_by_latency = true   # Route to lowest latency node
    max_redirects    = 3      # Handle MOVED/ASK redirects
  }
}
```

### When to Use Cluster

- Data doesn't fit in single node memory
- Need horizontal scaling for writes
- High throughput requirements
- Geographic distribution

### Cluster Considerations

- Keys are sharded by hash slot (16384 slots)
- Multi-key operations must use same slot (use `{hashtag}`)
- Minimum 3 master nodes recommended
- Each master should have at least 1 replica

## Redis Sentinel Mode

For automatic failover with master-replica setup.

```hcl
connector "redis_sentinel" {
  type   = "cache"
  driver = "redis"

  sentinel {
    nodes = [
      "sentinel-1:26379",
      "sentinel-2:26379",
      "sentinel-3:26379",
    ]

    master_name = "mymaster"
  }
}
```

### When to Use Sentinel

- High availability without sharding
- Automatic failover needed
- Simpler than cluster
- All data fits in single node

### Sentinel Setup

```yaml
# docker-compose.yml
services:
  redis-master:
    image: redis:7
    command: redis-server --appendonly yes

  redis-replica:
    image: redis:7
    command: redis-server --replicaof redis-master 6379

  sentinel:
    image: redis:7
    command: redis-sentinel /etc/sentinel.conf
    volumes:
      - ./sentinel.conf:/etc/sentinel.conf
```

```conf
# sentinel.conf
sentinel monitor mymaster redis-master 6379 2
sentinel down-after-milliseconds mymaster 5000
sentinel failover-timeout mymaster 60000
```

## Comparison

| Feature | Standalone | Sentinel | Cluster |
|---------|------------|----------|---------|
| High Availability | No | Yes | Yes |
| Automatic Failover | No | Yes | Yes |
| Horizontal Scaling | No | No | Yes |
| Multi-key Operations | Yes | Yes | Same slot only |
| Complexity | Low | Medium | High |
| Min Nodes | 1 | 3 (sentinels) | 6 (3 masters + 3 replicas) |

## Running Examples

### Cluster Mode

```bash
# Start Redis cluster
docker-compose -f docker-compose.cluster.yml up -d

# Set environment
export REDIS_NODE_1=localhost:7000
export REDIS_NODE_2=localhost:7001
export REDIS_NODE_3=localhost:7002

# Start Mycel
mycel start --config ./examples/redis-cluster
```

### Sentinel Mode

```bash
# Start Redis with Sentinel
docker-compose -f docker-compose.sentinel.yml up -d

# Set environment
export SENTINEL_1=localhost:26379
export SENTINEL_2=localhost:26380
export SENTINEL_3=localhost:26381
export REDIS_MASTER=mymaster

# Start Mycel
mycel start --config ./examples/redis-cluster
```

## Testing Failover

### Sentinel Failover

```bash
# Kill master
docker stop redis-master

# Sentinel promotes replica automatically
# Mycel reconnects to new master

# Check new master
redis-cli -p 26379 sentinel get-master-addr-by-name mymaster
```

### Cluster Failover

```bash
# Kill one master node
docker stop redis-node-1

# Cluster promotes replica automatically
# Data remains available (other slots unaffected)

# Check cluster status
redis-cli -c -p 7001 cluster info
```

## Connection Pool Settings

For high-throughput scenarios:

```hcl
connector "redis_cluster" {
  type   = "cache"
  driver = "redis"

  cluster { ... }

  # Connection pool
  pool_size    = 100   # Max connections
  min_idle     = 10    # Keep warm connections
  pool_timeout = "5s"  # Wait for connection

  # Timeouts
  dial_timeout  = "5s"
  read_timeout  = "3s"
  write_timeout = "3s"
}
```

## Monitoring

```bash
# Prometheus metrics
curl http://localhost:8080/metrics | grep redis

# mycel_cache_operations_total{driver="redis",operation="get"} 1234
# mycel_cache_operations_total{driver="redis",operation="set"} 567
# mycel_cache_hit_total 890
# mycel_cache_miss_total 344
```

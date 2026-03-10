# Observability Guide

This guide covers metrics, health checks, and logging in Mycel services.

## Overview

Mycel provides built-in observability features:

| Feature | Endpoint | Purpose |
|---------|----------|---------|
| Metrics | `/metrics` | Prometheus metrics |
| Health | `/health` | Detailed health status |
| Liveness | `/health/live` | Kubernetes liveness probe |
| Readiness | `/health/ready` | Kubernetes readiness probe |

## Prometheus Metrics

Mycel exposes metrics in Prometheus format at `/metrics`.

### Request Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_requests_total` | Counter | method, path, status | Total HTTP requests |
| `mycel_request_duration_seconds` | Histogram | method, path | Request duration |
| `mycel_requests_in_flight` | Gauge | method, path | Current in-flight requests |

### Flow Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_flow_executions_total` | Counter | flow, status | Total flow executions |
| `mycel_flow_duration_seconds` | Histogram | flow | Flow execution duration |
| `mycel_flow_errors_total` | Counter | flow, error_type | Flow errors by type |

### Connector Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_connector_health` | Gauge | connector, type | Health status (1=healthy) |
| `mycel_connector_operations_total` | Counter | connector, type, operation, status | Operations count |
| `mycel_connector_latency_seconds` | Histogram | connector, type, operation | Operation latency |

### Cache Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_cache_hits_total` | Counter | cache | Cache hits |
| `mycel_cache_misses_total` | Counter | cache | Cache misses |
| `mycel_cache_size` | Gauge | cache | Current cache size |

### Profile Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_connector_profile_active` | Gauge | connector, profile | Active profile (1=active) |
| `mycel_connector_profile_requests_total` | Counter | connector, profile | Requests per profile |
| `mycel_connector_profile_errors_total` | Counter | connector, profile, error | Errors per profile |
| `mycel_connector_profile_fallback_total` | Counter | connector, from, to | Fallback events |
| `mycel_connector_profile_latency_seconds` | Histogram | connector, profile | Latency per profile |

### Synchronization Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_lock_acquired_total` | Counter | key | Locks acquired |
| `mycel_lock_released_total` | Counter | key | Locks released |
| `mycel_lock_wait_seconds` | Histogram | key | Lock wait time |
| `mycel_lock_timeout_total` | Counter | key | Lock timeouts |
| `mycel_lock_held` | Gauge | key | Currently held locks |
| `mycel_semaphore_acquired_total` | Counter | key | Semaphore permits acquired |
| `mycel_semaphore_available` | Gauge | key | Available permits |
| `mycel_coordinate_signal_total` | Counter | signal | Signals emitted |
| `mycel_coordinate_wait_seconds` | Histogram | signal | Wait duration |

### Runtime Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mycel_uptime_seconds` | Gauge | - | Service uptime |
| `mycel_goroutines` | Gauge | - | Current goroutines |
| `mycel_service_info` | Gauge | service, version | Service metadata |
| `mycel_scheduled_flows` | Gauge | - | Scheduled flows count |

## Accessing Metrics

```bash
# Get all metrics
curl http://localhost:3000/metrics

# Filter specific metrics
curl http://localhost:3000/metrics | grep mycel_flow

# Get flow durations
curl http://localhost:3000/metrics | grep mycel_flow_duration
```

Example output:
```
# HELP mycel_requests_total Total number of HTTP requests processed
# TYPE mycel_requests_total counter
mycel_requests_total{method="GET",path="/users",status="200"} 150
mycel_requests_total{method="POST",path="/users",status="201"} 25

# HELP mycel_flow_duration_seconds Flow execution duration in seconds
# TYPE mycel_flow_duration_seconds histogram
mycel_flow_duration_seconds_bucket{flow="get_users",le="0.005"} 120
mycel_flow_duration_seconds_bucket{flow="get_users",le="0.01"} 145
mycel_flow_duration_seconds_sum{flow="get_users"} 0.45
mycel_flow_duration_seconds_count{flow="get_users"} 150
```

## Health Checks

### Detailed Health (`/health`)

Returns comprehensive status of all components:

```bash
curl http://localhost:3000/health
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "version": "1.0.0",
  "uptime": "2h30m15s",
  "components": [
    {
      "name": "postgres",
      "status": "healthy",
      "latency": "5ms"
    },
    {
      "name": "redis",
      "status": "healthy",
      "latency": "1ms"
    }
  ]
}
```

### Liveness Probe (`/health/live`)

Simple check that the process is alive. Always returns 200 unless crashed.

```bash
curl http://localhost:3000/health/live
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "version": "1.0.0",
  "uptime": "2h30m15s"
}
```

### Readiness Probe (`/health/ready`)

Checks if service is ready to receive traffic (all connectors healthy).

```bash
curl http://localhost:3000/health/ready
```

Response (healthy):
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

Response (not ready):
```json
{
  "status": "unhealthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "metadata": {
    "reason": "service not ready"
  }
}
```

## Kubernetes Configuration

### Deployment with Probes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-service
spec:
  template:
    spec:
      containers:
        - name: mycel
          image: ghcr.io/matutetandil/mycel:latest
          ports:
            - containerPort: 3000
          livenessProbe:
            httpGet:
              path: /health/live
              port: 3000
            initialDelaySeconds: 5
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /health/ready
              port: 3000
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 3
          resources:
            requests:
              memory: "64Mi"
              cpu: "100m"
            limits:
              memory: "256Mi"
              cpu: "500m"
```

### ServiceMonitor for Prometheus Operator

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: my-service
  labels:
    app: my-service
spec:
  selector:
    matchLabels:
      app: my-service
  endpoints:
    - port: http
      path: /metrics
      interval: 15s
```

## Logging

### Log Levels

| Level | Description | Use Case |
|-------|-------------|----------|
| `debug` | Detailed debugging | Development |
| `info` | Normal operations | Production (default) |
| `warn` | Warning conditions | Issues that may need attention |
| `error` | Error conditions | Failures that need investigation |

### Configuration

```bash
# Via command line
mycel start --log-level debug --log-format json

# Via environment variables
MYCEL_LOG_LEVEL=debug MYCEL_LOG_FORMAT=json mycel start
```

### Log Format

**Text format** (development):
```
2024-01-15T10:30:00.000Z INFO  Starting service: my-service
2024-01-15T10:30:00.001Z INFO  Loaded 3 connectors: api, db, cache
2024-01-15T10:30:00.002Z INFO  REST server listening on :3000
```

**JSON format** (production):
```json
{"time":"2024-01-15T10:30:00.000Z","level":"INFO","msg":"Starting service","service":"my-service"}
{"time":"2024-01-15T10:30:00.001Z","level":"INFO","msg":"Loaded connectors","count":3}
{"time":"2024-01-15T10:30:00.002Z","level":"INFO","msg":"REST server listening","port":3000}
```

## Grafana Dashboard

### Import Dashboard

1. Open Grafana
2. Go to Dashboards > Import
3. Use the JSON below or import from file

### Example Dashboard JSON

```json
{
  "title": "Mycel Service Dashboard",
  "panels": [
    {
      "title": "Request Rate",
      "type": "graph",
      "targets": [
        {
          "expr": "rate(mycel_requests_total[5m])",
          "legendFormat": "{{method}} {{path}}"
        }
      ]
    },
    {
      "title": "Request Duration (p95)",
      "type": "graph",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, rate(mycel_request_duration_seconds_bucket[5m]))",
          "legendFormat": "{{path}}"
        }
      ]
    },
    {
      "title": "Flow Errors",
      "type": "graph",
      "targets": [
        {
          "expr": "rate(mycel_flow_errors_total[5m])",
          "legendFormat": "{{flow}} - {{error_type}}"
        }
      ]
    },
    {
      "title": "Cache Hit Rate",
      "type": "stat",
      "targets": [
        {
          "expr": "sum(rate(mycel_cache_hits_total[5m])) / (sum(rate(mycel_cache_hits_total[5m])) + sum(rate(mycel_cache_misses_total[5m])))"
        }
      ]
    },
    {
      "title": "Connector Health",
      "type": "table",
      "targets": [
        {
          "expr": "mycel_connector_health",
          "legendFormat": "{{connector}}"
        }
      ]
    }
  ]
}
```

## Common Queries

### Request Rate
```promql
rate(mycel_requests_total[5m])
```

### Error Rate
```promql
rate(mycel_requests_total{status=~"5.."}[5m]) / rate(mycel_requests_total[5m])
```

### Request Duration (p95)
```promql
histogram_quantile(0.95, rate(mycel_request_duration_seconds_bucket[5m]))
```

### Slow Flows
```promql
histogram_quantile(0.99, rate(mycel_flow_duration_seconds_bucket[5m])) > 1
```

### Cache Hit Rate
```promql
sum(rate(mycel_cache_hits_total[5m])) /
(sum(rate(mycel_cache_hits_total[5m])) + sum(rate(mycel_cache_misses_total[5m])))
```

### Unhealthy Connectors
```promql
mycel_connector_health == 0
```

## Alerting Rules

### Example Prometheus Alerts

```yaml
groups:
  - name: mycel
    rules:
      - alert: HighErrorRate
        expr: rate(mycel_requests_total{status=~"5.."}[5m]) / rate(mycel_requests_total[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate on {{ $labels.path }}"
          description: "Error rate is {{ $value | humanizePercentage }}"

      - alert: SlowRequests
        expr: histogram_quantile(0.95, rate(mycel_request_duration_seconds_bucket[5m])) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Slow requests on {{ $labels.path }}"
          description: "p95 latency is {{ $value }}s"

      - alert: ConnectorUnhealthy
        expr: mycel_connector_health == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Connector {{ $labels.connector }} is unhealthy"

      - alert: LowCacheHitRate
        expr: |
          sum(rate(mycel_cache_hits_total[5m])) /
          (sum(rate(mycel_cache_hits_total[5m])) + sum(rate(mycel_cache_misses_total[5m]))) < 0.5
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Low cache hit rate"
          description: "Cache hit rate is {{ $value | humanizePercentage }}"

      - alert: HighLockContention
        expr: rate(mycel_lock_timeout_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High lock contention on {{ $labels.key }}"
```

## Docker Compose with Monitoring

```yaml
version: '3.8'

services:
  mycel:
    image: ghcr.io/matutetandil/mycel:latest
    ports:
      - "3000:3000"
    environment:
      - MYCEL_LOG_FORMAT=json

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3001:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana-data:/var/lib/grafana

volumes:
  grafana-data:
```

### prometheus.yml

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'mycel'
    static_configs:
      - targets: ['mycel:3000']
```

## Best Practices

1. **Use JSON logging in production** for easier parsing by log aggregators
2. **Set appropriate alert thresholds** based on your SLOs
3. **Monitor cache hit rates** - low rates indicate misconfigured cache keys
4. **Track connector latency** to identify slow dependencies
5. **Use readiness probes** to prevent routing traffic to unhealthy instances
6. **Set resource limits** based on observed metrics

## See Also

- [Configuration Reference](../reference/configuration.md) - Full HCL reference
- [Troubleshooting Guide](troubleshooting.md) - Common issues
- [Helm Chart](../../helm/mycel/README.md) - Kubernetes deployment

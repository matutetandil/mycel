# Mycel Helm Chart

A Helm chart for deploying [Mycel](https://github.com/matutetandil/mycel) - Declarative Microservice Framework on Kubernetes.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+
- PV provisioner support (optional, for persistent storage)

## Installing the Chart

### From Local Directory

```bash
# Clone the repository
git clone https://github.com/matutetandil/mycel.git
cd mycel

# Install with default values
helm install my-mycel ./helm/mycel

# Install with custom values
helm install my-mycel ./helm/mycel -f my-values.yaml

# Install in a specific namespace
helm install my-mycel ./helm/mycel -n mycel --create-namespace
```

### From OCI Registry (Future)

```bash
helm install my-mycel oci://ghcr.io/matutetandil/charts/mycel
```

## Uninstalling the Chart

```bash
helm uninstall my-mycel
```

## Configuration

### Basic Configuration

Create a `values.yaml` file:

```yaml
replicaCount: 2

mycel:
  env: production
  logLevel: info
  logFormat: json

  config:
    service: |
      service {
        name = "my-service"
        port = 8080
      }

    connectors: |
      connector "api" {
        type = "rest"
        port = 8080
      }

      connector "db" {
        type   = "database"
        driver = "postgres"
        host   = env("DB_HOST", "localhost")
        port   = 5432
        name   = env("DB_NAME", "mydb")
        user   = env("DB_USER", "postgres")
        password = env("DB_PASSWORD", "")
      }

    flows: |
      flow "get_users" {
        from {
          connector = "api"
          path      = "GET /users"
        }
        to {
          connector = "db"
          table     = "users"
        }
      }

  secrets:
    enabled: true
    env:
      DB_PASSWORD: "my-secret-password"
```

### Full Example with All Features

```yaml
replicaCount: 3

image:
  repository: mdenda/mycel
  tag: "1.0.0"

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: api.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: mycel-tls
      hosts:
        - api.example.com

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 80

mycel:
  env: production
  logLevel: info
  logFormat: json

  config:
    service: |
      service {
        name = "production-api"
        port = 8080
      }
    # ... more configuration

  secrets:
    enabled: true
    env:
      DATABASE_PASSWORD: "secret"
      API_KEY: "secret"

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: 30s

podDisruptionBudget:
  enabled: true
  minAvailable: 1
```

## Parameters

### Global Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `mdenda/mycel` |
| `image.tag` | Image tag | `""` (uses appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `imagePullSecrets` | Image pull secrets | `[]` |
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full name | `""` |

### Service Account Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |

### Pod Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podAnnotations` | Pod annotations | `{}` |
| `podLabels` | Pod labels | `{}` |
| `podSecurityContext` | Pod security context | `{fsGroup: 1000}` |
| `securityContext` | Container security context | See values.yaml |
| `resources` | Resource limits/requests | See values.yaml |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |

### Service Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `8080` |
| `service.targetPort` | Target port | `8080` |
| `service.nodePort` | Node port (if type=NodePort) | `""` |
| `service.annotations` | Service annotations | `{}` |

### Ingress Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `""` |
| `ingress.annotations` | Ingress annotations | `{}` |
| `ingress.hosts` | Ingress hosts | See values.yaml |
| `ingress.tls` | Ingress TLS configuration | `[]` |

### Autoscaling Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `autoscaling.enabled` | Enable HPA | `false` |
| `autoscaling.minReplicas` | Minimum replicas | `1` |
| `autoscaling.maxReplicas` | Maximum replicas | `10` |
| `autoscaling.targetCPUUtilizationPercentage` | Target CPU % | `80` |
| `autoscaling.targetMemoryUtilizationPercentage` | Target memory % | `""` |

### Mycel Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `mycel.env` | Environment | `production` |
| `mycel.logLevel` | Log level | `info` |
| `mycel.logFormat` | Log format | `json` |
| `mycel.hotReload` | Enable hot reload | `false` |
| `mycel.config.enabled` | Enable ConfigMap | `true` |
| `mycel.config.service` | service.hcl content | See values.yaml |
| `mycel.config.connectors` | connectors.hcl content | `""` |
| `mycel.config.flows` | flows.hcl content | `""` |
| `mycel.config.types` | types.hcl content | `""` |
| `mycel.config.extra` | Additional HCL files | `{}` |
| `mycel.secrets.enabled` | Enable secrets | `false` |
| `mycel.secrets.env` | Secret environment variables | `{}` |

### Health Check Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `healthCheck.enabled` | Enable health checks | `true` |
| `healthCheck.liveness.path` | Liveness probe path | `/health/live` |
| `healthCheck.readiness.path` | Readiness probe path | `/health/ready` |

### Metrics Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics | `true` |
| `metrics.port` | Metrics port | `8080` |
| `metrics.path` | Metrics path | `/metrics` |
| `metrics.serviceMonitor.enabled` | Enable ServiceMonitor | `false` |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` |

### Pod Disruption Budget Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podDisruptionBudget.enabled` | Enable PDB | `false` |
| `podDisruptionBudget.minAvailable` | Minimum available pods | `1` |
| `podDisruptionBudget.maxUnavailable` | Maximum unavailable pods | `""` |

## Examples

### Minimal Installation

```bash
helm install mycel ./helm/mycel
```

### Production Setup

```bash
helm install mycel ./helm/mycel \
  --set replicaCount=3 \
  --set mycel.env=production \
  --set autoscaling.enabled=true \
  --set metrics.serviceMonitor.enabled=true \
  --set podDisruptionBudget.enabled=true
```

### With External Database

```yaml
mycel:
  secrets:
    enabled: true
    env:
      DB_PASSWORD: "your-password"

  config:
    connectors: |
      connector "db" {
        type     = "database"
        driver   = "postgres"
        host     = "postgres.database.svc.cluster.local"
        port     = 5432
        name     = "mycel"
        user     = "mycel"
        password = env("DB_PASSWORD", "")
      }
```

## Upgrading

### To 0.2.0

No breaking changes.

### To 0.1.0

Initial release.

## License

MIT License

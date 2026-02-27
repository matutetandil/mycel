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

### From OCI Registry (Recommended)

```bash
# Install latest version
helm install my-mycel oci://ghcr.io/matutetandil/charts/mycel

# Install specific version
helm install my-mycel oci://ghcr.io/matutetandil/charts/mycel --version 1.0.0

# Install with custom values
helm install my-mycel oci://ghcr.io/matutetandil/charts/mycel -f values.yaml

# Install in a specific namespace
helm install my-mycel oci://ghcr.io/matutetandil/charts/mycel -n mycel --create-namespace
```

### Upgrade

```bash
# Upgrade to latest
helm upgrade my-mycel oci://ghcr.io/matutetandil/charts/mycel

# Upgrade with new values
helm upgrade my-mycel oci://ghcr.io/matutetandil/charts/mycel -f values.yaml
```

## Uninstalling the Chart

```bash
helm uninstall my-mycel
```

## Configuration

### Using Your HCL Files

If you already have `.hcl` configuration files (from local development, for example), you can load them directly with `--set-file` — no need to copy-paste into `values.yaml`:

```bash
helm install my-api ./helm/mycel \
  --set-file mycel.config.service=config.hcl \
  --set-file mycel.config.connectors=connectors.hcl \
  --set-file mycel.config.flows=flows.hcl \
  --set-file mycel.config.types=types.hcl
```

This keeps the same files you use during development, deployed unchanged into Kubernetes.

### Using an Existing ConfigMap

If you manage your Mycel configuration in a ConfigMap outside of this chart (e.g., via GitOps or a CI pipeline), point the chart at it:

```yaml
mycel:
  config:
    existingConfigMap: "my-mycel-config"
```

When `existingConfigMap` is set, the chart skips creating its own ConfigMap and mounts the one you specified. The ConfigMap must contain keys like `service.hcl`, `connectors.hcl`, etc.

### Basic Inline Configuration

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
| `mycel.config.existingConfigMap` | Use an existing ConfigMap instead of creating one | `""` |
| `mycel.config.service` | service.hcl content (supports `--set-file`) | See values.yaml |
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

- Added `mycel.config.existingConfigMap` to reference a pre-existing ConfigMap.
- Added `--set-file` documentation for loading HCL files directly.
- No breaking changes.

### To 0.1.0

Initial release.

## License

MIT License

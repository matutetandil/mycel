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

Mycel on Kubernetes works exactly like Mycel locally: the same `.hcl` files define your service. The Helm chart takes care of packaging them into a Kubernetes ConfigMap and mounting them at `/etc/mycel` so the Mycel binary can read them.

There are three ways to provide your HCL configuration, from simplest to most flexible.

### Option 1: From Your HCL Directory (Recommended)

Copy or symlink your project's `.hcl` files into the chart's `config/` directory. The chart auto-discovers every `.hcl` file and packages them into a ConfigMap — no listing, no flags.

**Example:** given a typical Mycel project:

```
my-service/
├── config.hcl
├── connectors/
│   ├── api.hcl
│   └── database.hcl
├── flows/
│   └── users.hcl
└── types/
    └── user.hcl
```

Deploy it:

```bash
# Copy the dev's HCL files into the chart
cp -r my-service/* helm/mycel/config/

# Deploy — Helm config in values.yaml, Mycel config auto-discovered from config/
helm install my-service ./helm/mycel -f values.yaml
```

The chart finds all `.hcl` files under `config/` recursively and flattens them into the ConfigMap (e.g., `connectors/api.hcl` becomes key `connectors_api.hcl`). Mycel reads them all regardless of filename.

> **Tip:** The `config/` directory is gitignored by default so each SysOps/environment can use different HCL files without committing them to the chart repo.

### Option 2: Inline HCL in values.yaml

Write the HCL content directly inside your `values.yaml`. Good for small services or when you want everything in a single file.

```yaml
replicaCount: 2

mycel:
  env: production
  logLevel: info

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
        type     = "database"
        driver   = "postgres"
        host     = env("DB_HOST", "localhost")
        port     = 5432
        name     = env("DB_NAME", "mydb")
        user     = env("DB_USER", "postgres")
        password = env("DB_PASSWORD", "")
      }

    flows: |
      flow "get_users" {
        from {
          connector = "api"
          operation = "GET /users"
        }
        to {
          connector = "db"
          target    = "users"
        }
      }

  secrets:
    enabled: true
    env:
      DB_PASSWORD: "my-secret-password"
```

### Option 3: Use an Existing ConfigMap

If you manage your own ConfigMap outside of this chart (e.g., via GitOps, Kustomize, or a CI pipeline), tell the chart to use it instead of creating one:

```yaml
mycel:
  config:
    existingConfigMap: "my-mycel-config"
```

The chart will skip creating its own ConfigMap and mount yours at `/etc/mycel`. Your ConfigMap must contain keys matching HCL filenames (e.g., `service.hcl`, `connectors.hcl`).

Create it from your project directory:

```bash
kubectl create configmap my-mycel-config \
  --from-file=service.hcl=config.hcl \
  --from-file=connectors.hcl=connectors/api.hcl \
  --from-file=flows.hcl=flows/users.hcl

helm install my-service ./helm/mycel \
  --set mycel.config.existingConfigMap=my-mycel-config
```

### Full Production Example

```yaml
replicaCount: 3

image:
  repository: mdenda/mycel
  tag: "1.0.0"

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
    # ... connectors, flows, types

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

### Deploy from HCL Directory

```bash
cp -r ./my-project/* ./helm/mycel/config/
helm install mycel ./helm/mycel -f values.yaml
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

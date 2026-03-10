# Kubernetes Deployment

## Helm Chart (Recommended)

Mycel provides an official Helm chart:

```bash
helm install my-api oci://ghcr.io/matutetandil/charts/mycel
```

### Quick Start

```bash
# Create ConfigMap from local HCL files
kubectl create configmap my-api-config --from-file=./config/

# Create Secret for credentials
kubectl create secret generic my-api-secrets \
  --from-literal=PG_PASSWORD=secret \
  --from-literal=API_TOKEN=sk-prod-token

# Install
helm install my-api oci://ghcr.io/matutetandil/charts/mycel \
  --set existingConfigMap=my-api-config \
  --set envFrom[0].secretRef.name=my-api-secrets
```

### Key Values

| Value | Default | Description |
|-------|---------|-------------|
| `image.tag` | `latest` | Mycel image tag |
| `existingConfigMap` | `""` | ConfigMap containing HCL files |
| `env` | `{}` | Static environment variables |
| `envFrom` | `[]` | Env from Secrets/ConfigMaps |
| `replicaCount` | `1` | Number of replicas |
| `resources.limits.memory` | `256Mi` | Memory limit |
| `resources.requests.cpu` | `100m` | CPU request |
| `autoscaling.enabled` | `false` | Enable HPA |
| `ingress.enabled` | `false` | Enable Ingress |

See [helm/mycel/README.md](../../helm/mycel/README.md) for the complete values reference.

## Manual Deployment

### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-api-config
data:
  config.hcl: |
    service {
      name    = "orders-api"
      version = "1.0.0"
    }
  connectors.hcl: |
    connector "api" {
      type = "rest"
      port = 3000
    }
    connector "db" {
      type     = "database"
      driver   = "postgres"
      host     = env("PG_HOST")
      database = env("PG_DATABASE")
      user     = env("PG_USER")
      password = env("PG_PASSWORD")
    }
  flows.hcl: |
    flow "get_orders" {
      from {
        connector = "api"
        operation = "GET /orders"
      }
      to {
        connector = "db"
        target    = "orders"
      }
    }
```

### Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-api-secrets
stringData:
  PG_PASSWORD: "production-password"
  JWT_SECRET: "jwt-secret-key"
  STRIPE_KEY: "sk_live_..."
```

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-api
  template:
    metadata:
      labels:
        app: my-api
    spec:
      containers:
        - name: mycel
          image: ghcr.io/matutetandil/mycel:v1.7.0
          ports:
            - containerPort: 3000
              name: http
            - containerPort: 9090
              name: admin
          env:
            - name: MYCEL_ENV
              value: production
            - name: MYCEL_LOG_FORMAT
              value: json
            - name: PG_HOST
              value: postgres.default.svc.cluster.local
            - name: PG_DATABASE
              value: myapp
            - name: PG_USER
              value: app
          envFrom:
            - secretRef:
                name: my-api-secrets
          volumeMounts:
            - name: config
              mountPath: /etc/mycel
          livenessProbe:
            httpGet:
              path: /health/live
              port: 3000
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /health/ready
              port: 3000
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "256Mi"
      volumes:
        - name: config
          configMap:
            name: my-api-config
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-api
spec:
  selector:
    app: my-api
  ports:
    - name: http
      port: 80
      targetPort: 3000
    - name: admin
      port: 9090
      targetPort: 9090
```

### HPA (Horizontal Pod Autoscaling)

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: my-api
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: my-api
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

## Health Probes

For services with a REST connector, use the REST port:

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 3000

readinessProbe:
  httpGet:
    path: /health/ready
    port: 3000
```

For services without a REST connector (queue workers, CDC pipelines), use the admin port:

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 9090

readinessProbe:
  httpGet:
    path: /health/ready
    port: 9090
```

## Prometheus Scraping

Add annotations to scrape metrics:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/path: "/metrics"
    prometheus.io/port: "3000"
```

## See Also

- [Docker Deployment](docker.md)
- [Production Checklist](production.md)
- [Helm Chart README](../../helm/mycel/README.md)

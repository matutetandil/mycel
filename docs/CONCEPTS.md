# Conceptos de Mycel

Este documento define todos los conceptos de Mycel y su configuración.

---

## 1. Conceptos Core

### Connector

**Qué es:** Adaptador bidireccional que conecta Mycel con un sistema externo (base de datos, API, queue, archivo, etc).

**Modos de operación:**
- **Input (Source):** Recibe datos o eventos que disparan un flow. Ejemplos: endpoint REST expuesto, mensaje consumido de una queue, request gRPC entrante.
- **Output (Target):** Destino donde el flow escribe datos. Ejemplos: INSERT en base de datos, llamada HTTP a otra API, publicar mensaje en queue.

**Nota:** Algunos connectors son exclusivamente input (ej: cron), otros exclusivamente output (ej: email/notificaciones), y la mayoría pueden ser ambos según el contexto.

---

### Flow

**Qué es:** Unidad de trabajo que define el camino de los datos. Conecta un input con un output, opcionalmente transformando los datos en el medio.

**Estructura:**
```hcl
flow "nombre" {
  from { connector.input = "trigger" }   # Qué dispara el flow

  transform { ... }                       # Opcional: transformar datos

  to { connector.output = "destino" }    # Dónde van los datos
}
```

**Cuándo se ejecuta:** Cuando el connector de `from` recibe un evento (request HTTP, mensaje de queue, etc) o según el trigger configurado (cron, interval).

---

### Transform

**Qué es:** Transformación de datos usando expresiones CEL (Common Expression Language). Mapea campos de entrada a campos de salida.

**Modos:**
- **Inline:** Definido dentro del flow, para uso único.
- **Reusable:** Definido en archivo separado, referenciado con `use`.
- **Composición:** Combinar múltiples transforms con override.

```hcl
# Inline
transform {
  output.id = "uuid()"
  output.email = "lower(input.email)"
  output.created_at = "now()"
}

# Reusable con composición
transform {
  use = [transform.normalize_user, transform.add_timestamps]
  output.source = "'api'"  # Override
}
```

---

### Type

**Qué es:** Schema que define la estructura esperada de los datos. Valida campos, tipos, y formatos.

**Uso:** Validar input de un flow, o output antes de enviarlo.

```hcl
type "user" {
  id       = string { required = true }
  email    = string { format = "email" }
  age      = number { min = 0, max = 150 }
  role     = string { enum = ["admin", "user", "guest"] }
  metadata = object { optional = true }
}
```

**Validación en flow:**
```hcl
flow "create_user" {
  from { ... }
  input_type = type.user    # Valida entrada
  output_type = type.user   # Valida salida
  to { ... }
}
```

---

### Validator

**Qué es:** Regla de validación custom para campos que requieren lógica especial más allá de los tipos built-in.

**Tipos:**
- **regex:** Patrón de expresión regular
- **cel:** Expresión CEL que retorna true/false
- **wasm:** Módulo WASM compilado para validaciones complejas

```hcl
# Regex
validator "cuit_argentina" {
  type    = "regex"
  pattern = "^(20|23|24|27|30|33|34)\\d{8}\\d$"
  message = "CUIT inválido"
}

# CEL
validator "adult" {
  type    = "cel"
  expr    = "value >= 18"
  message = "Debe ser mayor de edad"
}

# Uso en type
type "customer" {
  cuit = string { validate = validator.cuit_argentina }
  age  = number { validate = validator.adult }
}
```

---

## 2. Connectors por Tipo

### REST

**Qué es:** Protocolo HTTP para APIs web. El connector más común.

#### Como Input (Server) - Exponer endpoints

Mycel actúa como servidor HTTP, exponiendo endpoints que disparan flows.

```hcl
connector "api" {
  type = "rest"
  mode = "server"  # Implícito cuando tiene port

  port = 8080
  host = "0.0.0.0"  # Opcional, default 0.0.0.0

  # TLS
  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }

  # CORS
  cors {
    origins = ["https://app.example.com"]
    methods = ["GET", "POST", "PUT", "DELETE"]
    headers = ["Authorization", "Content-Type"]
    credentials = true
    max_age = 3600
  }

  # Rate limiting global
  rate_limit {
    requests = 100
    window   = "1m"
    by       = "ip"  # ip, header, query
  }

  # Autenticación entrante (validar requests)
  # GAP: No implementado aún
  auth {
    type = "jwt"  # jwt, api_key, basic, oauth2

    # Para JWT
    jwt {
      secret      = env("JWT_SECRET")      # O jwks_url
      jwks_url    = "https://..."          # Para validar con JWKS
      issuer      = "https://auth.example.com"
      audience    = ["my-api"]
      algorithms  = ["RS256", "HS256"]
    }

    # Para API Key
    api_key {
      header = "X-API-Key"           # O query param
      keys   = [env("API_KEY_1")]    # Lista de keys válidas
      # O validar contra DB/servicio externo
      validate = { connector.keys_db = "api_keys" }
    }

    # Para Basic Auth
    basic {
      users = {
        admin = env("ADMIN_PASSWORD")
      }
      # O validar contra DB
      validate = { connector.users_db = "users" }
    }

    # Rutas públicas (sin auth)
    public = ["/health", "/metrics", "/docs/*"]
  }

  # Headers requeridos
  # GAP: No implementado
  required_headers = ["X-Request-ID", "X-Correlation-ID"]

  # Headers que se agregan a todas las respuestas
  # GAP: No implementado
  response_headers {
    "X-Powered-By" = "Mycel"
    "X-Request-ID" = "${request.id}"
  }
}
```

#### Como Output (Client) - Llamar APIs externas

Mycel actúa como cliente HTTP, llamando a APIs externas.

```hcl
connector "external_api" {
  type = "rest"
  mode = "client"  # Implícito cuando tiene base_url

  base_url = env("EXTERNAL_API_URL")

  # Timeout y retry
  timeout = "30s"
  retry {
    attempts = 3
    backoff  = "exponential"  # exponential, linear, constant
    initial  = "1s"
    max      = "30s"
  }

  # Autenticación saliente (para autenticarse con la API)
  # GAP: Solo básico implementado, falta OAuth2, API Key dinámico
  auth {
    type = "bearer"  # bearer, basic, api_key, oauth2, custom

    # Bearer token estático
    bearer {
      token = env("API_TOKEN")
    }

    # Basic auth
    basic {
      username = env("API_USER")
      password = env("API_PASS")
    }

    # API Key
    api_key {
      header = "X-API-Key"       # O "query" para query param
      name   = "api_key"         # Nombre del param si es query
      value  = env("API_KEY")
    }

    # OAuth2 Client Credentials
    # GAP: No implementado
    oauth2 {
      grant_type    = "client_credentials"
      token_url     = "https://auth.example.com/oauth/token"
      client_id     = env("CLIENT_ID")
      client_secret = env("CLIENT_SECRET")
      scopes        = ["read", "write"]
      # Token caching automático
    }

    # OAuth2 con refresh token
    # GAP: No implementado
    oauth2 {
      grant_type    = "refresh_token"
      token_url     = "https://auth.example.com/oauth/token"
      refresh_token = env("REFRESH_TOKEN")
      client_id     = env("CLIENT_ID")
    }

    # Custom header
    custom {
      headers = {
        "X-Custom-Auth" = env("CUSTOM_TOKEN")
        "X-Tenant-ID"   = "tenant-123"
      }
    }
  }

  # Headers estáticos para todas las requests
  headers {
    "Accept"       = "application/json"
    "User-Agent"   = "Mycel/1.0"
    "X-Request-ID" = "${uuid()}"  # Dinámico por request
  }

  # Circuit breaker
  circuit_breaker {
    threshold         = 5      # Fallos para abrir
    timeout           = "30s"  # Tiempo en open antes de half-open
    success_threshold = 2      # Éxitos para cerrar
  }

  # TLS personalizado (para APIs con certs custom)
  # GAP: No implementado
  tls {
    ca_cert             = "/path/to/ca.pem"
    client_cert         = "/path/to/client-cert.pem"
    client_key          = "/path/to/client-key.pem"
    insecure_skip_verify = false  # Solo para desarrollo
  }
}
```

**Uso en flows:**
```hcl
# Como input (server)
flow "get_users" {
  from { connector.api = "GET /users" }
  to   { connector.database = "users" }
}

# Como output (client)
flow "sync_to_external" {
  from { connector.database = "SELECT * FROM users WHERE synced = false" }
  to   { connector.external_api = "POST /users" }
}
```

---

### Database (SQL)

**Qué es:** Conexión a bases de datos relacionales (PostgreSQL, MySQL, SQLite).

**Drivers:** `postgres`, `mysql`, `sqlite`

#### Configuración común

```hcl
connector "db" {
  type   = "database"
  driver = "postgres"  # postgres, mysql, sqlite

  # Conexión
  host     = env("DB_HOST")
  port     = 5432
  database = env("DB_NAME")
  username = env("DB_USER")
  password = env("DB_PASS")

  # O connection string
  # dsn = env("DATABASE_URL")

  # Pool de conexiones
  pool {
    max_open = 25       # Conexiones máximas abiertas
    max_idle = 5        # Conexiones idle máximas
    max_lifetime = "1h" # Tiempo máximo de vida de una conexión
  }

  # SSL/TLS
  ssl {
    mode     = "require"  # disable, require, verify-ca, verify-full
    ca_cert  = "/path/to/ca.pem"
    cert     = "/path/to/client-cert.pem"
    key      = "/path/to/client-key.pem"
  }

  # Read replica (para queries de lectura)
  # GAP: No implementado
  replica {
    host     = env("DB_REPLICA_HOST")
    port     = 5432
    # Hereda credenciales del principal
  }

  # Schema default
  schema = "public"  # PostgreSQL
}
```

#### Como Input (leer datos)

```hcl
flow "get_users" {
  from { connector.api = "GET /users" }
  to   { connector.db = "users" }  # SELECT * FROM users
}

flow "get_user_by_id" {
  from { connector.api = "GET /users/:id" }
  to   { connector.db = "users WHERE id = :id" }
}

# Query raw
flow "complex_query" {
  from { connector.api = "GET /reports/sales" }
  to   {
    connector.db = <<SQL
      SELECT
        date_trunc('month', created_at) as month,
        SUM(amount) as total
      FROM orders
      WHERE status = 'completed'
      GROUP BY 1
      ORDER BY 1
    SQL
  }
}
```

#### Como Output (escribir datos)

```hcl
flow "create_user" {
  from { connector.api = "POST /users" }
  to   { connector.db = "INSERT users" }
}

flow "update_user" {
  from { connector.api = "PUT /users/:id" }
  to   { connector.db = "UPDATE users WHERE id = :id" }
}

flow "delete_user" {
  from { connector.api = "DELETE /users/:id" }
  to   { connector.db = "DELETE users WHERE id = :id" }
}
```

---

### Database (NoSQL - MongoDB)

**Qué es:** Conexión a MongoDB.

```hcl
connector "mongo" {
  type   = "database"
  driver = "mongodb"

  # Conexión
  uri      = env("MONGO_URI")  # mongodb://user:pass@host:27017/db
  database = "myapp"

  # O por partes
  host     = env("MONGO_HOST")
  port     = 27017
  username = env("MONGO_USER")
  password = env("MONGO_PASS")

  # Opciones
  auth_source  = "admin"
  replica_set  = "rs0"

  # Pool
  pool {
    min = 5
    max = 100
  }

  # TLS
  tls {
    enabled  = true
    ca_cert  = "/path/to/ca.pem"
  }
}
```

**Uso:**
```hcl
# Input: leer documentos
flow "get_products" {
  from { connector.api = "GET /products" }
  to   { connector.mongo = "products" }  # find en collection
}

# Output: escribir documentos
flow "create_product" {
  from { connector.api = "POST /products" }
  to   { connector.mongo = "INSERT products" }
}

# Query con filtro
flow "get_active_products" {
  from { connector.api = "GET /products/active" }
  to   {
    connector.mongo = {
      collection = "products"
      filter     = { status = "active" }
      sort       = { created_at = -1 }
      limit      = 100
    }
  }
}
```

---

### Message Queue (RabbitMQ)

**Qué es:** Conexión a RabbitMQ para mensajería asíncrona.

#### Configuración

```hcl
connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"

  # Conexión
  host     = env("RABBIT_HOST")
  port     = 5672
  username = env("RABBIT_USER")
  password = env("RABBIT_PASS")
  vhost    = "/"

  # O connection string
  # url = "amqp://user:pass@host:5672/vhost"

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # Heartbeat y timeouts
  heartbeat        = "10s"
  connection_timeout = "30s"

  # Exchange default
  exchange {
    name    = "myapp"
    type    = "topic"      # direct, topic, fanout, headers
    durable = true
    auto_delete = false
  }

  # Prefetch (para consumers)
  prefetch = 10

  # Reconexión automática
  reconnect {
    enabled  = true
    interval = "5s"
    max_attempts = 0  # 0 = infinito
  }
}
```

#### Como Input (Consumer) - Leer mensajes

Mycel consume mensajes de una queue y ejecuta el flow.

```hcl
flow "process_order" {
  from {
    connector.rabbit = {
      queue = "orders"

      # Configuración de la queue
      durable     = true
      auto_delete = false
      exclusive   = false

      # Binding (de qué exchange/routing key viene)
      bind {
        exchange    = "myapp"
        routing_key = "order.created"
      }

      # Consumer
      consumer_tag = "mycel-orders"
      auto_ack     = false  # Manual ack después de procesar

      # DLQ para mensajes fallidos
      # GAP: Parcialmente implementado
      dlq {
        enabled     = true
        queue       = "orders.dlq"
        exchange    = "myapp.dlq"
        routing_key = "order.failed"
        max_retries = 3
      }

      # Parseo del mensaje
      format = "json"  # json, msgpack, protobuf, raw
    }
  }

  to { connector.db = "INSERT orders" }
}
```

**Acceso a headers del mensaje:**
```hcl
transform {
  # El mensaje viene estructurado así:
  # input.body    = contenido del mensaje
  # input.headers = headers AMQP
  # input.properties = properties (correlation_id, message_id, etc)

  order_id       = "input.body.id"
  correlation_id = "input.properties.correlation_id"
  source         = "input.headers.x_source"
}
```

#### Como Output (Producer) - Publicar mensajes

```hcl
flow "notify_order_created" {
  from { connector.db = "SELECT * FROM orders WHERE notified = false" }

  to {
    connector.rabbit = {
      exchange    = "myapp"
      routing_key = "order.created"

      # Propiedades del mensaje
      persistent  = true       # Mensaje durable
      mandatory   = true       # Error si no hay queue que lo reciba

      # Headers custom
      headers {
        "x-source"   = "mycel"
        "x-priority" = "high"
      }

      # Properties AMQP
      content_type   = "application/json"
      correlation_id = "${input.id}"
      message_id     = "${uuid()}"
      expiration     = "3600000"  # TTL en ms

      # Formato de serialización
      format = "json"
    }
  }
}
```

---

### Message Queue (Kafka)

**Qué es:** Conexión a Apache Kafka para streaming de eventos.

#### Configuración

```hcl
connector "kafka" {
  type   = "queue"
  driver = "kafka"

  # Brokers
  brokers = [
    env("KAFKA_BROKER_1"),
    env("KAFKA_BROKER_2"),
    env("KAFKA_BROKER_3")
  ]

  # Autenticación
  # GAP: Solo SASL_PLAIN implementado
  auth {
    mechanism = "SASL_PLAIN"  # SASL_PLAIN, SASL_SCRAM_256, SASL_SCRAM_512
    username  = env("KAFKA_USER")
    password  = env("KAFKA_PASS")
  }

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # Para Confluent Cloud u otros servicios managed
  # GAP: No implementado
  schema_registry {
    url      = "https://schema-registry.example.com"
    username = env("SR_USER")
    password = env("SR_PASS")
  }
}
```

#### Como Input (Consumer)

```hcl
flow "process_events" {
  from {
    connector.kafka = {
      topic = "events"

      # Consumer group
      group_id = "mycel-events"

      # Offset inicial
      offset = "earliest"  # earliest, latest, timestamp

      # Particiones específicas (opcional)
      partitions = [0, 1, 2]

      # Commit automático o manual
      auto_commit = false

      # Batch processing
      # GAP: No implementado
      batch {
        enabled  = true
        size     = 100
        timeout  = "5s"
      }

      # Formato
      format = "json"  # json, avro, protobuf
    }
  }

  to { connector.db = "INSERT events" }
}
```

**Acceso a metadata:**
```hcl
transform {
  # input.body      = contenido del mensaje
  # input.key       = key del mensaje
  # input.headers   = headers Kafka
  # input.partition = número de partición
  # input.offset    = offset del mensaje
  # input.timestamp = timestamp del mensaje

  event_id  = "input.body.id"
  event_key = "input.key"
  partition = "input.partition"
}
```

#### Como Output (Producer)

```hcl
flow "emit_event" {
  from { connector.api = "POST /events" }

  to {
    connector.kafka = {
      topic = "events"

      # Key para particionamiento
      key = "${input.user_id}"  # Mensajes del mismo user van a misma partición

      # Headers
      headers {
        "event-type" = "${input.type}"
        "source"     = "mycel"
      }

      # Partición específica (override del key-based)
      # partition = 0

      # Acks
      acks = "all"  # 0, 1, all

      # Formato
      format = "json"

      # Compression
      compression = "snappy"  # none, gzip, snappy, lz4, zstd
    }
  }
}
```

---

### gRPC

**Qué es:** Protocolo RPC de alto rendimiento basado en Protocol Buffers.

#### Como Input (Server) - Exponer servicios gRPC

```hcl
connector "grpc_server" {
  type = "grpc"
  mode = "server"

  port = 50051

  # Proto files
  proto {
    path = "./protos"           # Directorio con .proto files
    files = ["service.proto"]   # O archivos específicos
  }

  # TLS
  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }

  # Reflection (para herramientas como grpcurl)
  reflection = true

  # Health check gRPC estándar
  health_check = true

  # Interceptors
  # GAP: No implementado
  interceptors {
    auth {
      type = "jwt"
      # ... config similar a REST
    }
    logging = true
    metrics = true
  }

  # Max message size
  max_recv_message_size = "4MB"
  max_send_message_size = "4MB"
}
```

#### Como Output (Client) - Llamar servicios gRPC

```hcl
connector "grpc_client" {
  type = "grpc"
  mode = "client"

  address = env("GRPC_SERVICE_ADDR")  # host:port

  # Proto
  proto {
    path = "./protos"
  }

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # Sin TLS (desarrollo)
  # insecure = true

  # Auth
  # GAP: No implementado
  auth {
    type = "bearer"
    token = env("GRPC_TOKEN")
  }

  # Timeout y retry
  timeout = "30s"
  retry {
    attempts = 3
  }

  # Load balancing
  # GAP: No implementado
  load_balancing = "round_robin"  # round_robin, pick_first
}
```

---

### TCP

**Qué es:** Conexión TCP directa para protocolos custom o legacy.

#### Como Input (Server)

```hcl
connector "tcp_server" {
  type = "tcp"
  mode = "server"

  port = 9000
  host = "0.0.0.0"

  # Protocolo de mensajes
  protocol = "json"  # json, msgpack, line, length_prefixed, nestjs

  # Para length_prefixed
  length_prefix {
    size   = 4       # Bytes del prefijo
    endian = "big"   # big, little
  }

  # Para line protocol
  line {
    delimiter = "\n"
    max_length = 65536
  }

  # TLS
  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }

  # Conexiones
  max_connections = 1000
  read_timeout    = "30s"
  write_timeout   = "30s"
}
```

#### Como Output (Client)

```hcl
connector "tcp_client" {
  type = "tcp"
  mode = "client"

  host = env("TCP_SERVER_HOST")
  port = 9000

  protocol = "json"

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # Timeouts
  connect_timeout = "10s"
  read_timeout    = "30s"
  write_timeout   = "30s"

  # Reconexión
  reconnect {
    enabled  = true
    interval = "5s"
  }

  # Pool de conexiones
  pool {
    size = 10
  }
}
```

---

### Files

**Qué es:** Lectura/escritura de archivos locales.

```hcl
connector "files" {
  type = "file"

  # Directorio base
  base_path = "/data"

  # Permisos para archivos nuevos
  file_mode = "0644"
  dir_mode  = "0755"
}
```

**Uso:**
```hcl
# Leer archivo
flow "import_data" {
  from { connector.files = "input/data.json" }
  to   { connector.db = "INSERT data" }
}

# Escribir archivo
flow "export_report" {
  from { connector.db = "SELECT * FROM reports" }
  to   {
    connector.files = {
      path   = "output/report.json"
      format = "json"     # json, csv, text, lines
      append = false
    }
  }
}

# Con template en nombre
flow "daily_export" {
  from { connector.db = "SELECT * FROM orders WHERE date = today()" }
  to   {
    connector.files = {
      path = "exports/orders_${date('YYYY-MM-DD')}.csv"
      format = "csv"
      csv {
        delimiter = ","
        header    = true
      }
    }
  }
}
```

---

### S3

**Qué es:** Almacenamiento de objetos compatible con S3 (AWS, MinIO, etc).

```hcl
connector "s3" {
  type = "s3"

  # AWS S3
  region     = "us-east-1"
  bucket     = env("S3_BUCKET")
  access_key = env("AWS_ACCESS_KEY")
  secret_key = env("AWS_SECRET_KEY")

  # O S3-compatible (MinIO, etc)
  endpoint = "http://minio:9000"

  # Path style (para MinIO)
  force_path_style = true

  # Prefix default para todos los objetos
  prefix = "mycel/"
}
```

**Uso:**
```hcl
# Leer objeto
flow "import_from_s3" {
  from { connector.s3 = "data/input.json" }
  to   { connector.db = "INSERT data" }
}

# Escribir objeto
flow "backup_to_s3" {
  from { connector.db = "SELECT * FROM users" }
  to   {
    connector.s3 = {
      key     = "backups/users_${date('YYYY-MM-DD')}.json"
      format  = "json"

      # Metadata
      metadata {
        "x-backup-type" = "daily"
      }

      # Storage class
      storage_class = "STANDARD_IA"  # STANDARD, STANDARD_IA, GLACIER, etc
    }
  }
}

# Generar presigned URL
flow "get_download_url" {
  from { connector.api = "GET /files/:key/url" }
  to   {
    connector.s3 = {
      operation = "presign_get"
      key       = "${input.key}"
      expires   = "1h"
    }
  }
}
```

---

### Cache

**Qué es:** Almacenamiento en caché para acelerar accesos frecuentes.

**Drivers:** `memory`, `redis`

```hcl
# Memory (local, para desarrollo o single-instance)
connector "cache" {
  type   = "cache"
  driver = "memory"

  # Límites
  max_size = "100MB"
  max_items = 10000

  # TTL default
  ttl = "10m"

  # Eviction policy
  eviction = "lru"  # lru, lfu
}

# Redis (distribuido)
connector "cache" {
  type   = "cache"
  driver = "redis"

  host     = env("REDIS_HOST")
  port     = 6379
  password = env("REDIS_PASS")
  db       = 0

  # Prefix para keys
  prefix = "mycel:"

  # Cluster
  # GAP: No implementado
  cluster {
    enabled = true
    nodes   = ["redis1:6379", "redis2:6379", "redis3:6379"]
  }

  # Sentinel
  # GAP: No implementado
  sentinel {
    master = "mymaster"
    nodes  = ["sentinel1:26379", "sentinel2:26379"]
  }
}
```

**Uso en flows:**
```hcl
flow "get_product" {
  cache {
    storage = "connector.cache"
    key     = "'product:' + input.id"
    ttl     = "5m"
  }

  from { connector.api = "GET /products/:id" }
  to   { connector.db = "products WHERE id = :id" }
}
```

---

### Exec

**Qué es:** Ejecutar comandos del sistema o scripts.

```hcl
connector "exec" {
  type = "exec"

  # Directorio de trabajo
  working_dir = "/app/scripts"

  # Variables de entorno adicionales
  env {
    PATH = "/usr/local/bin:/usr/bin"
    MY_VAR = "value"
  }

  # Timeout default
  timeout = "60s"

  # Shell
  shell = "/bin/bash"

  # SSH remoto
  # GAP: Implementado pero limitado
  ssh {
    host     = env("SSH_HOST")
    port     = 22
    user     = env("SSH_USER")
    key_file = "/path/to/key"
    # O password
    # password = env("SSH_PASS")
  }
}
```

**Uso:**
```hcl
flow "run_script" {
  from { connector.api = "POST /jobs/run" }
  to   {
    connector.exec = {
      command = "./process.sh"
      args    = ["${input.file}", "${input.mode}"]
      timeout = "5m"
    }
  }
}

# Pipe output
flow "get_stats" {
  from { connector.api = "GET /stats" }
  to   {
    connector.exec = {
      command = "df -h | grep /dev/sda"
      shell   = true  # Ejecutar en shell
    }
  }
}
```

---

### GraphQL

**Qué es:** Protocolo de consulta flexible para APIs.

#### Como Input (Server) - Exponer API GraphQL

```hcl
connector "graphql" {
  type = "graphql"
  mode = "server"

  port = 8080
  path = "/graphql"

  # Schema SDL
  schema = <<SDL
    type Query {
      users: [User!]!
      user(id: ID!): User
    }

    type Mutation {
      createUser(input: CreateUserInput!): User!
    }

    type User {
      id: ID!
      email: String!
      name: String
    }

    input CreateUserInput {
      email: String!
      name: String
    }
  SDL

  # O desde archivo
  # schema_file = "./schema.graphql"

  # Playground/GraphiQL
  playground = true

  # Auth (similar a REST)
  # GAP: No integrado
  auth {
    type = "jwt"
    # ...
  }

  # Introspection
  introspection = true  # false en producción

  # Límites
  max_depth       = 10
  max_complexity  = 1000
}
```

#### Como Output (Client) - Llamar APIs GraphQL

```hcl
connector "graphql_client" {
  type = "graphql"
  mode = "client"

  endpoint = env("GRAPHQL_ENDPOINT")

  # Auth
  auth {
    type = "bearer"
    token = env("GRAPHQL_TOKEN")
  }

  # Headers
  headers {
    "X-Custom" = "value"
  }

  # Timeout
  timeout = "30s"
}
```

**Uso:**
```hcl
# Query
flow "get_external_users" {
  from { connector.api = "GET /external-users" }
  to   {
    connector.graphql_client = {
      query = <<GRAPHQL
        query GetUsers($limit: Int) {
          users(limit: $limit) {
            id
            name
            email
          }
        }
      GRAPHQL
      variables {
        limit = 100
      }
    }
  }
}

# Mutation
flow "create_external_user" {
  from { connector.api = "POST /external-users" }
  to   {
    connector.graphql_client = {
      query = <<GRAPHQL
        mutation CreateUser($input: CreateUserInput!) {
          createUser(input: $input) {
            id
            email
          }
        }
      GRAPHQL
      variables {
        input = "${input}"
      }
    }
  }
}
```

---

## 3. Sincronización

### Lock (Mutex)

**Qué es:** Exclusión mutua distribuida. Garantiza que solo un flow procese un recurso específico a la vez.

**Cuándo usarlo:**
- Evitar procesamiento duplicado del mismo pedido
- Operaciones que no pueden ser concurrentes (ej: actualizar saldo)

```hcl
flow "process_order" {
  lock {
    key     = "'order:' + input.order_id"
    storage = "connector.redis"
    timeout = "30s"

    # Qué hacer si no se puede adquirir el lock
    on_fail = "wait"  # wait, skip, fail
    wait_timeout = "10s"
  }

  from { connector.rabbit = "orders" }
  to   { connector.db = "UPDATE orders SET status = 'processing'" }
}
```

---

### Semaphore

**Qué es:** Limitar concurrencia a N ejecuciones simultáneas.

**Cuándo usarlo:**
- Rate limiting hacia APIs externas que tienen límites
- Limitar carga en recursos compartidos

```hcl
flow "call_external_api" {
  semaphore {
    key     = "external_api"
    permits = 5  # Máximo 5 requests concurrentes
    storage = "connector.redis"
    timeout = "30s"

    on_fail = "wait"
    wait_timeout = "1m"
  }

  from { connector.rabbit = "api_calls" }
  to   { connector.external_api = "POST /endpoint" }
}
```

---

### Coordinate (Signal/Wait)

**Qué es:** Coordinar ejecución entre flows dependientes. Un flow espera hasta que otro señalice.

**Cuándo usarlo:**
- Procesar items hijo solo después de que el padre existe
- Sincronizar flujos paralelos

```hcl
# Flow que procesa el parent y señaliza
flow "process_order" {
  from { connector.rabbit = "orders" }

  signal {
    key     = "'order:' + input.id"
    storage = "connector.redis"
  }

  to { connector.db = "INSERT orders" }
}

# Flow que espera al parent
flow "process_order_item" {
  wait {
    key     = "'order:' + input.order_id"
    storage = "connector.redis"
    timeout = "5m"

    # Verificación previa en DB
    check {
      connector.db = "SELECT 1 FROM orders WHERE id = :order_id"
    }

    # Qué hacer si timeout
    on_timeout = "retry"  # fail, skip, retry, dlq
    max_retries = 3
  }

  from { connector.rabbit = "order_items" }
  to   { connector.db = "INSERT order_items" }
}
```

---

### Flow Triggers (when)

**Qué es:** Definir cuándo se ejecuta un flow además del trigger normal del `from`.

```hcl
# Por defecto: se ejecuta cuando llega algo al from
flow "on_request" {
  from { connector.api = "GET /data" }
  to   { connector.db = "data" }
}

# Cron: ejecutar en horarios específicos
flow "daily_report" {
  when = "0 3 * * *"  # 3am todos los días

  from { connector.db = "SELECT * FROM sales WHERE date = yesterday()" }
  to   { connector.email = "reports@example.com" }
}

# Interval: ejecutar cada X tiempo
flow "health_check" {
  when = "@every 1m"

  from { connector.external_api = "GET /health" }
  to   { connector.metrics = "health_status" }
}

# Shortcuts
flow "weekly_cleanup" {
  when = "@weekly"  # @hourly, @daily, @weekly, @monthly

  from { connector.db = "DELETE FROM logs WHERE created_at < now() - interval '30 days'" }
  to   { connector.logs = "cleanup_complete" }
}
```

---

## 4. Extensibilidad

### Functions (WASM)

**Qué es:** Funciones custom compiladas a WASM que se pueden usar en expresiones CEL.

**Cuándo usarlo:**
- Lógica de negocio compleja que no se puede expresar en CEL
- Algoritmos específicos (pricing, scoring, etc)

```hcl
functions "pricing" {
  wasm    = "./wasm/pricing.wasm"
  exports = ["calculate_price", "apply_discount", "calculate_tax"]
}
```

**Uso en transforms:**
```hcl
transform {
  subtotal = "calculate_price(input.items)"
  discount = "apply_discount(subtotal, input.coupon)"
  tax      = "calculate_tax(subtotal - discount, input.country)"
  total    = "subtotal - discount + tax"
}
```

---

### Plugins

**Qué es:** Extensiones que agregan nuevos tipos de connectors via WASM.

**Cuándo usarlo:**
- Integrar sistemas no soportados nativamente (Salesforce, SAP, etc)
- Protocolos propietarios

```hcl
# Declarar plugin
plugin "salesforce" {
  source  = "./plugins/salesforce"  # O "registry/salesforce"
  version = "1.0.0"
}

# Usar connector del plugin
connector "sf" {
  type = "salesforce"

  instance_url = env("SF_URL")
  client_id    = env("SF_CLIENT_ID")
  client_secret = env("SF_CLIENT_SECRET")
}
```

---

### Aspects (AOP)

**Qué es:** Cross-cutting concerns aplicados automáticamente a múltiples flows por pattern matching.

**Cuándo usarlo:**
- Audit logging en todas las operaciones de escritura
- Cache automática en todas las lecturas
- Métricas custom en todos los flows

```hcl
aspect "audit_log" {
  # Cuándo ejecutar
  when = "after"  # before, after, around, on_error

  # A qué flows aplicar (glob patterns)
  on = [
    "flows/**/create_*.hcl",
    "flows/**/update_*.hcl",
    "flows/**/delete_*.hcl"
  ]

  # Excluir
  except = ["flows/internal/*"]

  # Acción a ejecutar
  action {
    connector.audit_db = {
      operation = "INSERT audit_logs"
      data = {
        flow       = "${flow.name}"
        user       = "${context.user.id}"
        action     = "${flow.operation}"
        input      = "${json(input)}"
        output     = "${json(output)}"
        timestamp  = "${now()}"
      }
    }
  }
}

aspect "cache_reads" {
  when = "around"
  on   = ["flows/**/get_*.hcl", "flows/**/list_*.hcl"]

  cache {
    storage = "connector.cache"
    key     = "'flow:' + flow.name + ':' + hash(input)"
    ttl     = "5m"
  }
}
```

---

## 5. Authentication System

**Qué es:** Sistema de autenticación enterprise-grade declarativo.

### Configuración básica

```hcl
auth {
  # Preset base (strict, standard, relaxed, development)
  preset = "standard"

  # JWT
  jwt {
    secret         = env("JWT_SECRET")
    issuer         = "mycel"
    audience       = ["my-api"]
    access_ttl     = "15m"
    refresh_ttl    = "7d"
    algorithm      = "HS256"  # HS256, RS256, ES256
  }

  # Password
  password {
    min_length     = 8
    require_upper  = true
    require_lower  = true
    require_number = true
    require_special = true
    check_breach   = true  # Check HaveIBeenPwned
  }

  # Sessions
  session {
    max_per_user   = 5
    idle_timeout   = "30m"
    absolute_timeout = "24h"
  }

  # Brute force protection
  brute_force {
    max_attempts   = 5
    lockout_time   = "15m"
    progressive_delay = true
  }

  # MFA
  mfa {
    enabled  = true
    required = false  # true = obligatorio para todos

    totp {
      issuer = "MyApp"
      digits = 6
      period = 30
    }

    webauthn {
      rp_name = "MyApp"
      rp_id   = "myapp.com"
    }

    recovery_codes {
      count = 10
    }
  }

  # Storage
  storage {
    users    = "connector.db"  # Tabla users
    sessions = "connector.redis"
    tokens   = "connector.redis"
  }

  # Endpoints (opcionales, defaults razonables)
  endpoints {
    login           = "POST /auth/login"
    logout          = "POST /auth/logout"
    register        = "POST /auth/register"
    refresh         = "POST /auth/refresh"
    change_password = "POST /auth/password"
    mfa_setup       = "POST /auth/mfa/setup"
    mfa_verify      = "POST /auth/mfa/verify"
  }
}
```

---

## Resumen de GAPs Identificados

### REST Input (Server)
- [x] Auth entrante (JWT, API Key, Basic, OAuth2 validation) ✅
- [x] Required headers validation ✅
- [x] Response headers custom ✅

### REST Output (Client)
- [x] OAuth2 client credentials con token refresh ✅
- [x] OAuth2 refresh token flow ✅
- [x] TLS con client certificates ✅
- [ ] Dynamic API key (from DB/service)

### Database
- [x] Read replica routing ✅ (PostgreSQL y MySQL)

### Message Queues
- [x] DLQ completo con retry count ✅ (RabbitMQ)
- [x] Kafka SASL_SCRAM authentication ✅
- [x] Kafka Schema Registry integration ✅
- [x] Kafka batch processing ✅

### gRPC
- [x] Server interceptors (auth, logging) ✅ (JWT, API Key, mTLS)
- [x] Client auth ✅ (Bearer, API Key, OAuth2)
- [ ] Load balancing

### Cache
- [x] Redis Cluster ✅
- [x] Redis Sentinel ✅

### General
- [ ] Aspects (AOP) - no implementado
- [ ] Múltiples implementaciones de Sync (Phase 4.2)

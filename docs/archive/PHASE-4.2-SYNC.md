# Fase 4.2 - Sincronización: Locks, Coordinate y Headers de MQ

## Contexto

Mycel es un framework declarativo de microservicios en Go. Ya tiene implementados connectors para RabbitMQ y Kafka. Necesitamos agregar mecanismos de sincronización para coordinar el procesamiento de mensajes.

## Caso de Uso Real

Procesamiento de productos de e-commerce desde RabbitMQ:
- Llegan mensajes CREATE/UPDATE de productos "configurables" y "simples"
- Los productos simples tienen un `parent_id` que referencia a un configurable
- El CREATE de un simple debe esperar a que su configurable padre exista
- El orden de llegada está garantizado (configurable siempre llega primero)
- Actualmente se usa un mutex global que serializa TODO (lento)

**Objetivo:** Permitir procesamiento paralelo donde sea posible, pero respetando dependencias.

---

## Parte 1: Acceso a Headers de Mensajes MQ

### Estructura del Input para MQ

Los mensajes de MQ exponen una estructura completa:

```
input.body              # payload parseado (JSON)
input.headers           # headers del mensaje AMQP/Kafka
input.properties        # propiedades AMQP (message_id, timestamp, content_type, etc)
input.routing_key       # routing key (RabbitMQ)
input.topic             # topic (Kafka)
input.partition         # partition (Kafka)
input.offset            # offset (Kafka)
input.key               # message key (Kafka)
```

### Tareas

1. Modificar `internal/connector/mq/rabbitmq/consumer.go` para exponer estructura completa
2. Modificar `internal/connector/mq/kafka/consumer.go` para exponer estructura completa
3. Actualizar tests existentes que asumen `input` = body
4. Actualizar ejemplos en `examples/mq/`

### Uso en Flows

```hcl
flow "process_product" {
  from { connector.rabbitmq = "queue:products" }

  transform {
    # Acceso a headers
    output.type      = input.headers.type
    output.parent_id = input.headers.parent_id

    # Acceso a body
    output.sku       = input.body.sku
    output.name      = input.body.name

    # Acceso a properties
    output.msg_id    = input.properties.message_id
  }

  to { connector.postgres = "products" }
}
```

### Tests

- Test que verifica acceso a headers en RabbitMQ
- Test que verifica acceso a headers en Kafka
- Test de transform usando headers

---

## Parte 2: Lock (Mutex por Key)

### Especificación HCL

```hcl
flow "process_payment" {
  from { connector.rabbitmq = "queue:payments" }

  lock {
    storage = connector.redis_sync   # storage para locks
    key     = "user:" + input.body.user_id  # expresión CEL
    timeout = "30s"                  # max tiempo con lock
    wait    = true                   # true=espera, false=falla si locked
    retry   = "100ms"                # intervalo de retry si wait=true
  }

  to { connector.postgres = "payments" }
}
```

### Comportamiento

1. Antes de ejecutar el flow, intenta adquirir lock en `key`
2. Si `wait=true` y lock ocupado → reintenta cada `retry` hasta `timeout`
3. Si `wait=false` y lock ocupado → falla inmediatamente
4. Ejecuta el flow
5. Libera el lock (siempre, incluso si el flow falla)

### Implementación

```go
// internal/sync/lock.go
package sync

type Lock interface {
    Acquire(ctx context.Context, key string, timeout time.Duration) (bool, error)
    Release(ctx context.Context, key string) error
}

type LockConfig struct {
    Storage  string        // referencia al connector
    Key      string        // expresión CEL
    Timeout  time.Duration
    Wait     bool
    Retry    time.Duration
}
```

### Estructura de Archivos

```
internal/sync/
├── lock.go              # Interface y config
├── lock_redis.go        # Implementación Redis (SET NX PX)
├── lock_memory.go       # Implementación memory (para dev/test)
└── lock_test.go
```

### Implementación Redis

Usar `SET key value NX PX milliseconds`:

```go
func (l *RedisLock) Acquire(ctx context.Context, key string, timeout time.Duration) (bool, error) {
    return l.client.SetNX(ctx, l.prefix+key, l.instanceID, timeout).Result()
}

func (l *RedisLock) Release(ctx context.Context, key string) error {
    // Solo liberar si somos el owner (Lua script para atomicidad)
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `
    return l.client.Eval(ctx, script, []string{l.prefix + key}, l.instanceID).Err()
}
```

### Métricas

```go
mycel_lock_acquired_total{key="..."}
mycel_lock_released_total{key="..."}
mycel_lock_wait_seconds{key="..."}
mycel_lock_timeout_total{key="..."}
```

---

## Parte 2.5: Semaphore (Limitar Concurrencia)

### Especificación HCL

```hcl
flow "call_external_api" {
  from { connector.rabbitmq = "queue:requests" }

  semaphore {
    storage     = connector.redis_sync
    key         = "external_api"       # nombre del semáforo
    max_permits = 10                   # máximo 10 en paralelo
    timeout     = "30s"                # max tiempo esperando permit
    lease       = "60s"                # max tiempo con permit (auto-release)
  }

  to { connector.external_api = "POST /process" }
}
```

### Casos de Uso

| Caso | Config |
|------|--------|
| API externa con rate limit | `max_permits = 10` (max 10 req paralelos) |
| Pool de conexiones limitado | `max_permits = 5` |
| Proteger recurso costoso | `max_permits = 3` |

### Comportamiento

1. Antes de ejecutar, intenta adquirir un permit
2. Si hay permits disponibles → adquiere y continúa
3. Si no hay permits → espera hasta `timeout`
4. Ejecuta el flow
5. Libera el permit (siempre, incluso si falla)
6. Si pasa `lease` sin liberar → auto-release (protección contra crashes)

### Implementación

```go
// internal/sync/semaphore.go
package sync

type Semaphore interface {
    Acquire(ctx context.Context, key string, timeout, lease time.Duration) (string, error)  // returns permitID
    Release(ctx context.Context, key string, permitID string) error
    Available(ctx context.Context, key string) (int, error)
}

type SemaphoreConfig struct {
    Storage    string
    Key        string        // expresión CEL
    MaxPermits int
    Timeout    time.Duration
    Lease      time.Duration
}
```

### Implementación Redis (Sorted Set + Lua)

Usar sorted set donde el score es el timestamp de expiración:

```go
func (s *RedisSemaphore) Acquire(ctx context.Context, key string, timeout, lease time.Duration) (string, error) {
    permitID := uuid.New().String()
    deadline := time.Now().Add(timeout)

    script := `
        -- Limpiar permits expirados
        redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])

        -- Contar permits activos
        local count = redis.call('ZCARD', KEYS[1])

        -- Si hay espacio, agregar permit
        if count < tonumber(ARGV[2]) then
            redis.call('ZADD', KEYS[1], ARGV[3], ARGV[4])
            return 1
        end
        return 0
    `

    now := time.Now().UnixMilli()
    expireAt := time.Now().Add(lease).UnixMilli()

    for time.Now().Before(deadline) {
        result, err := s.client.Eval(ctx, script,
            []string{s.prefix + key},
            now,                    // ARGV[1] - current time
            s.maxPermits,           // ARGV[2] - max permits
            expireAt,               // ARGV[3] - expire score
            permitID,               // ARGV[4] - permit ID
        ).Int()

        if err != nil {
            return "", err
        }
        if result == 1 {
            return permitID, nil
        }

        time.Sleep(50 * time.Millisecond)
    }

    return "", ErrSemaphoreTimeout
}

func (s *RedisSemaphore) Release(ctx context.Context, key string, permitID string) error {
    return s.client.ZRem(ctx, s.prefix+key, permitID).Err()
}
```

### Métricas

```go
mycel_semaphore_acquired_total{key="..."}
mycel_semaphore_released_total{key="..."}
mycel_semaphore_wait_seconds{key="..."}
mycel_semaphore_timeout_total{key="..."}
mycel_semaphore_available{key="..."}
```

### Diferencia: Lock vs Semaphore

| Feature | Lock (Mutex) | Semaphore |
|---------|--------------|-----------|
| Permits | 1 | N configurable |
| Uso | Exclusión mutua | Limitar concurrencia |
| Key | Por recurso específico | Por grupo/pool |
| Ejemplo | `user:123` (un update a la vez) | `api:external` (max 10 paralelos) |

---

## Parte 3: Coordinate (Esperar Señal)

Coordinate permite coordinar dependencias entre procesos: uno espera (`wait`) hasta que otro señalice (`signal`). Aplica a cualquier escenario de dependencia:
- Productos configurables/simples (e-commerce)
- Órdenes padre/hijas
- Documentos con adjuntos
- Procesos batch que dependen de inicialización
- Workflows con pasos dependientes

### Especificación HCL

```hcl
flow "process_entity" {
  from { connector.rabbitmq = "queue:entities" }

  coordinate {
    # Quién espera
    wait {
      when = "input.headers.type == 'child'"    # expresión CEL
      for  = "'parent:' + input.headers.parent_id + ':ready'"  # expresión CEL
    }

    # Quién activa
    signal {
      when = "input.headers.type == 'parent'"   # expresión CEL
      emit = "'parent:' + input.body.id + ':ready'"  # expresión CEL
      ttl  = "5m"
    }

    # Check previo - si ya existe en DB, no espera
    preflight {
      connector = connector.postgres
      query     = "SELECT 1 FROM entities WHERE id = :parent_id"
      params    = { parent_id = "input.headers.parent_id" }
      if_exists = "pass"
    }

    # Config general
    storage              = connector.redis
    timeout              = "60s"
    on_timeout           = "fail"   # fail | retry | skip | pass
    max_retries          = 3        # solo aplica si on_timeout = "retry"
    max_concurrent_waits = 10       # max waits activos simultáneamente
  }

  to { connector.postgres = "entities" }
}
```

### Comportamiento

**Flujo completo:**

```
Mensaje llega
     │
     ▼
¿Aplica wait.when? ─── No ───► ¿Aplica signal.when? ─── Sí ───► Emite signal
     │                                │
    Sí                               No
     │                                │
     ▼                                ▼
Preflight: ¿existe                  Pasa directo
en DB?
     │
  ┌──┴──┐
  │     │
 Sí    No
  │     │
  ▼     ▼
Pasa   ¿Hay lugar? (max_concurrent_waits)
       │
    ┌──┴──┐
    │     │
   Sí    No
    │     │
    ▼     ▼
 Espera  Espera permit
 signal  primero
    │
 ┌──┴──┐
 │     │
Llega  Timeout
 │     │
 ▼     ▼
Pasa   on_timeout
```

**Valores de `on_timeout`:**

| Valor | Comportamiento |
|-------|----------------|
| `fail` | Error, no procesa el mensaje |
| `retry` | Requeue (max `max_retries` veces), luego DLQ |
| `skip` | No procesa, pero no es error (ack silencioso) |
| `pass` | Deja pasar igual, procesa sin la señal |

### Implementación

```go
// internal/sync/coordinate.go
package sync

type Coordinator interface {
    Wait(ctx context.Context, signal string, timeout time.Duration) (bool, error)
    Signal(ctx context.Context, signal string, ttl time.Duration) error
    Exists(ctx context.Context, signal string) (bool, error)
}

type CoordinateConfig struct {
    Storage            string
    Wait               *WaitConfig
    Signal             *SignalConfig
    Preflight          *PreflightConfig
    Timeout            time.Duration
    OnTimeout          string  // fail | retry | skip | pass
    MaxRetries         int     // para on_timeout = "retry"
    MaxConcurrentWaits int     // max waits simultáneos (0 = unlimited)
}

type WaitConfig struct {
    When string  // expresión CEL
    For  string  // expresión CEL para la señal a esperar
}

type SignalConfig struct {
    When string  // expresión CEL
    Emit string  // expresión CEL para la señal a emitir
    TTL  time.Duration
}

type PreflightConfig struct {
    Connector string
    Query     string
    Params    map[string]string  // param -> expresión CEL
    IfExists  string  // "pass" | "fail"
}
```

### Implementación Redis con CoordinatorHub (Pub/Sub Eficiente)

Un solo subscriber global que maneja todos los waiters:

```go
// internal/sync/coordinate_hub.go
package sync

type CoordinatorHub struct {
    client  *redis.Client
    prefix  string

    mu      sync.RWMutex
    waiters map[string][]chan struct{}  // signal -> waiting channels

    sub     *redis.PubSub
    done    chan struct{}
}

func NewCoordinatorHub(client *redis.Client, prefix string) *CoordinatorHub {
    hub := &CoordinatorHub{
        client:  client,
        prefix:  prefix,
        waiters: make(map[string][]chan struct{}),
        done:    make(chan struct{}),
    }

    // Un solo subscriber para todos los signals
    hub.sub = client.PSubscribe(context.Background(), prefix+"signal:*")
    go hub.listen()

    return hub
}

func (h *CoordinatorHub) listen() {
    ch := h.sub.Channel()
    for {
        select {
        case msg := <-ch:
            // Extraer signal name del channel
            signal := strings.TrimPrefix(msg.Channel, h.prefix+"signal:")
            h.notify(signal)
        case <-h.done:
            return
        }
    }
}

func (h *CoordinatorHub) notify(signal string) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if waiters, ok := h.waiters[signal]; ok {
        for _, ch := range waiters {
            close(ch)
        }
        delete(h.waiters, signal)
    }
}

func (h *CoordinatorHub) Signal(ctx context.Context, signal string, ttl time.Duration) error {
    pipe := h.client.Pipeline()
    pipe.Set(ctx, h.prefix+signal, "1", ttl)
    pipe.Publish(ctx, h.prefix+"signal:"+signal, "ready")
    _, err := pipe.Exec(ctx)
    return err
}

func (h *CoordinatorHub) Wait(ctx context.Context, signal string, timeout time.Duration) (bool, error) {
    // 1. Check si ya existe
    exists, err := h.client.Exists(ctx, h.prefix+signal).Result()
    if err != nil {
        return false, err
    }
    if exists > 0 {
        return true, nil
    }

    // 2. Registrar waiter ANTES de double-check
    ch := make(chan struct{})
    h.mu.Lock()
    h.waiters[signal] = append(h.waiters[signal], ch)
    h.mu.Unlock()

    // Cleanup en caso de timeout/cancel
    defer func() {
        h.mu.Lock()
        waiters := h.waiters[signal]
        for i, w := range waiters {
            if w == ch {
                h.waiters[signal] = append(waiters[:i], waiters[i+1:]...)
                break
            }
        }
        if len(h.waiters[signal]) == 0 {
            delete(h.waiters, signal)
        }
        h.mu.Unlock()
    }()

    // 3. Double-check después de registrar
    exists, err = h.client.Exists(ctx, h.prefix+signal).Result()
    if err != nil {
        return false, err
    }
    if exists > 0 {
        return true, nil
    }

    // 4. Esperar pasivamente
    select {
    case <-ch:
        return true, nil
    case <-time.After(timeout):
        return false, nil
    case <-ctx.Done():
        return false, ctx.Err()
    }
}

func (h *CoordinatorHub) Close() error {
    close(h.done)
    return h.sub.Close()
}
```

### Implementación Memory con Cleanup

```go
// internal/sync/coordinate_memory.go
package sync

type MemoryCoordinator struct {
    mu       sync.RWMutex
    signals  map[string]time.Time       // signal -> expiresAt
    waiters  map[string][]chan struct{} // signal -> waiting channels

    done     chan struct{}
}

func NewMemoryCoordinator(cleanupInterval time.Duration) *MemoryCoordinator {
    c := &MemoryCoordinator{
        signals: make(map[string]time.Time),
        waiters: make(map[string][]chan struct{}),
        done:    make(chan struct{}),
    }

    // Goroutine de cleanup periódico
    go c.cleanupLoop(cleanupInterval)

    return c
}

func (c *MemoryCoordinator) cleanupLoop(interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            c.cleanExpired()
        case <-c.done:
            return
        }
    }
}

func (c *MemoryCoordinator) cleanExpired() {
    c.mu.Lock()
    defer c.mu.Unlock()

    now := time.Now()
    for signal, expiresAt := range c.signals {
        if now.After(expiresAt) {
            delete(c.signals, signal)
        }
    }
}

func (c *MemoryCoordinator) Signal(ctx context.Context, signal string, ttl time.Duration) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.signals[signal] = time.Now().Add(ttl)

    // Notificar waiters
    if waiters, ok := c.waiters[signal]; ok {
        for _, ch := range waiters {
            close(ch)
        }
        delete(c.waiters, signal)
    }
    return nil
}

func (c *MemoryCoordinator) Wait(ctx context.Context, signal string, timeout time.Duration) (bool, error) {
    c.mu.Lock()

    // Check si existe y no expiró
    if expiresAt, ok := c.signals[signal]; ok && time.Now().Before(expiresAt) {
        c.mu.Unlock()
        return true, nil
    }

    // Crear canal para esperar
    ch := make(chan struct{})
    c.waiters[signal] = append(c.waiters[signal], ch)
    c.mu.Unlock()

    select {
    case <-ch:
        return true, nil
    case <-time.After(timeout):
        c.mu.Lock()
        // Cleanup waiter
        waiters := c.waiters[signal]
        for i, w := range waiters {
            if w == ch {
                c.waiters[signal] = append(waiters[:i], waiters[i+1:]...)
                break
            }
        }
        c.mu.Unlock()
        return false, nil
    case <-ctx.Done():
        return false, ctx.Err()
    }
}

func (c *MemoryCoordinator) Close() error {
    close(c.done)
    return nil
}
```

### Métricas

```go
mycel_coordinate_signal_total{signal="..."}
mycel_coordinate_wait_total{signal="..."}
mycel_coordinate_wait_seconds{signal="..."}
mycel_coordinate_timeout_total{signal="..."}
mycel_coordinate_preflight_hit_total{connector="..."}
mycel_coordinate_active_waits{signal="..."}
```

### Estructura de Archivos

```
internal/sync/
├── lock.go              # Interface y config
├── lock_redis.go
├── lock_memory.go
├── lock_test.go
├── semaphore.go         # Interface y config
├── semaphore_redis.go   # Sorted set + Lua
├── semaphore_memory.go
├── semaphore_test.go
├── coordinate.go        # Interface y config
├── coordinate_hub.go    # Redis con pub/sub hub
├── coordinate_memory.go # Memory con cleanup
├── coordinate_test.go
└── metrics.go           # Métricas Prometheus
```

---

## Parte 4: Flow Triggers (`when`)

### Especificación HCL

```hcl
# Flow normal - trigger por request/mensaje (default)
flow "get_users" {
  # when = "always"  # implícito, se puede omitir
  from { connector.api = "GET /users" }
  to   { connector.db = "users" }
}

# Flow con schedule cron
flow "daily_cleanup" {
  when = "0 3 * * *"  # cron estándar (5 campos)

  # Sin "from" - el trigger es el schedule
  to {
    connector = "postgres"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

# Flow con interval
flow "health_ping" {
  when = "@every 5m"

  to { connector.monitoring = "POST /ping" }
}

# Flow que combina schedule + source
flow "batch_process" {
  when = "0 */2 * * *"  # cada 2 horas
  from { connector.rabbitmq = "queue:pending" }  # consume lo acumulado
  to   { connector.db = "processed" }
}
```

### Valores de `when`

| Valor | Significado |
|-------|-------------|
| `"always"` | Default. Trigger por `from` (request/mensaje) |
| `"0 3 * * *"` | Cron estándar (minuto hora día mes díaSemana) |
| `"@every 5m"` | Interval (5m, 1h, 30s, etc.) |
| `"@hourly"` | Shortcut: `0 * * * *` |
| `"@daily"` | Shortcut: `0 0 * * *` |
| `"@weekly"` | Shortcut: `0 0 * * 0` |
| `"@monthly"` | Shortcut: `0 0 1 * *` |

### Implementación

```go
// internal/flow/flow.go
type Config struct {
    Name      string
    When      string  // "always" | cron expression | "@every X"
    From      *FromConfig
    To        *ToConfig
    // ... resto
}

// internal/scheduler/scheduler.go
type Scheduler struct {
    cron    *cron.Cron  // robfig/cron
    flows   map[string]cron.EntryID
}

func (s *Scheduler) Schedule(flow *flow.Config, handler func()) error {
    switch {
    case flow.When == "" || flow.When == "always":
        return nil  // No schedule, trigger por from
    case strings.HasPrefix(flow.When, "@every "):
        interval := strings.TrimPrefix(flow.When, "@every ")
        // Parse interval y agregar al cron
    default:
        // Asumir cron expression
        entryID, err := s.cron.AddFunc(flow.When, handler)
        if err != nil {
            return err
        }
        s.flows[flow.Name] = entryID
    }
    return nil
}
```

### Dependencia

```go
// go.mod
require github.com/robfig/cron/v3 v3.0.1
```

---

## Parte 5: Integración en Runtime

### Modificar flow_registry.go

```go
func (r *FlowRegistry) executeFlow(ctx context.Context, flow *Flow, input map[string]any) (any, error) {
    // 1. Evaluar y adquirir Lock si está configurado
    if flow.Lock != nil {
        key, err := r.evaluateCEL(flow.Lock.Key, input)
        if err != nil {
            return nil, err
        }
        acquired, err := r.lockManager.Acquire(ctx, key.(string), flow.Lock.Timeout)
        if err != nil {
            return nil, err
        }
        if !acquired {
            if flow.Lock.Wait {
                return nil, ErrLockTimeout
            }
            return nil, ErrLockBusy
        }
        defer r.lockManager.Release(ctx, key.(string))
    }

    // 2. Evaluar y adquirir Semaphore si está configurado
    var permitID string
    if flow.Semaphore != nil {
        key, err := r.evaluateCEL(flow.Semaphore.Key, input)
        if err != nil {
            return nil, err
        }
        permitID, err = r.semaphoreManager.Acquire(ctx, key.(string), flow.Semaphore.Timeout, flow.Semaphore.Lease)
        if err != nil {
            return nil, fmt.Errorf("semaphore acquire failed: %w", err)
        }
        defer r.semaphoreManager.Release(ctx, key.(string), permitID)
    }

    // 3. Evaluar Coordinate si está configurado
    if flow.Coordinate != nil {
        if err := r.handleCoordinate(ctx, flow.Coordinate, input); err != nil {
            return nil, err
        }
    }

    // 4. Ejecutar flow normal
    result, err := r.doExecute(ctx, flow, input)
    if err != nil {
        return nil, err
    }

    // 5. Emitir Signal si está configurado
    if flow.Coordinate != nil && flow.Coordinate.Signal != nil {
        if err := r.handleSignal(ctx, flow.Coordinate, input); err != nil {
            log.Printf("signal emit failed: %v", err)
        }
    }

    return result, nil
}

func (r *FlowRegistry) handleCoordinate(ctx context.Context, coord *CoordinateConfig, input map[string]any) error {
    if coord.Wait == nil {
        return nil
    }

    // Evaluar condición wait.when
    shouldWait, err := r.evaluateCELBool(coord.Wait.When, input)
    if err != nil {
        return err
    }

    if !shouldWait {
        return nil
    }

    // Preflight check
    if coord.Preflight != nil {
        exists, err := r.checkPreflight(ctx, coord.Preflight, input)
        if err != nil {
            return err
        }
        if exists && coord.Preflight.IfExists == "pass" {
            return nil
        }
    }

    // Adquirir permit si hay max_concurrent_waits
    if coord.MaxConcurrentWaits > 0 {
        permitID, err := r.coordinateLimiter.Acquire(ctx, coord.Storage, coord.MaxConcurrentWaits, coord.Timeout)
        if err != nil {
            return fmt.Errorf("max concurrent waits reached: %w", err)
        }
        defer r.coordinateLimiter.Release(ctx, coord.Storage, permitID)
    }

    // Evaluar signal name
    signal, err := r.evaluateCEL(coord.Wait.For, input)
    if err != nil {
        return err
    }

    // Esperar señal
    ok, err := r.coordinator.Wait(ctx, signal.(string), coord.Timeout)
    if err != nil {
        return err
    }

    if !ok {
        switch coord.OnTimeout {
        case "retry":
            retryCount := getRetryCount(input)
            if retryCount >= coord.MaxRetries {
                return ErrMaxRetriesExceeded
            }
            return ErrRetry
        case "skip":
            return ErrSkip
        case "pass":
            return nil
        default:
            return ErrCoordinateTimeout
        }
    }

    return nil
}

func (r *FlowRegistry) handleSignal(ctx context.Context, coord *CoordinateConfig, input map[string]any) error {
    if coord.Signal == nil {
        return nil
    }

    shouldSignal, err := r.evaluateCELBool(coord.Signal.When, input)
    if err != nil {
        return err
    }

    if !shouldSignal {
        return nil
    }

    signal, err := r.evaluateCEL(coord.Signal.Emit, input)
    if err != nil {
        return err
    }

    return r.coordinator.Signal(ctx, signal.(string), coord.Signal.TTL)
}
```

---

## Parte 6: Sync Storage Connector

Reutilizar connector de cache existente:

```hcl
connector "redis" {
  type   = "cache"
  driver = "redis"
  url    = env("REDIS_URL")
}

# Locks, semaphores y coordinate usan el mismo connector
lock {
  storage = connector.redis
  ...
}

coordinate {
  storage = connector.redis
  ...
}
```

---

## Tests

### Unit Tests

1. `lock_test.go`
   - Test acquire/release
   - Test timeout
   - Test concurrent locks same key (should block)
   - Test concurrent locks different keys (should not block)

2. `semaphore_test.go`
   - Test acquire/release con max_permits
   - Test timeout cuando no hay permits
   - Test lease expiration (auto-release)

3. `coordinate_test.go`
   - Test signal/wait
   - Test wait timeout con cada on_timeout
   - Test signal TTL expiration
   - Test max_retries
   - Test max_concurrent_waits
   - Test preflight

4. `headers_test.go`
   - Test RabbitMQ message con input.body, input.headers
   - Test Kafka message con input.body, input.headers, input.key

5. `scheduler_test.go`
   - Test cron expression parsing
   - Test @every interval
   - Test shortcuts (@daily, @hourly)

### Integration Tests

1. Test completo del caso de uso:
   - Enviar CREATE parent
   - Enviar CREATE child (debe esperar)
   - Verificar que child esperó al parent
   - Verificar que ambos se procesaron correctamente

2. Test de paralelismo:
   - Enviar CREATE parent-A
   - Enviar CREATE parent-B
   - Verificar que se procesan en paralelo

3. Test max_retries:
   - Enviar child sin parent
   - Verificar que reintenta max_retries veces
   - Verificar que va a DLQ

---

## Ejemplo Completo

```
examples/sync/
├── README.md
├── config.mycel
├── connectors/
│   ├── rabbitmq.mycel
│   ├── postgres.mycel
│   └── redis.mycel
├── flows/
│   ├── process_entity.mycel
│   ├── call_external.mycel      # semaphore example
│   └── daily_cleanup.mycel      # cron example
└── docker-compose.yml
```

### flows/process_entity.mycel

```hcl
flow "process_entity" {
  from { connector.rabbitmq = "queue:entities" }

  coordinate {
    wait {
      when = "input.headers.type == 'child'"
      for  = "'entity:' + input.headers.parent_id + ':ready'"
    }

    signal {
      when = "input.headers.type == 'parent'"
      emit = "'entity:' + input.body.id + ':ready'"
      ttl  = "5m"
    }

    preflight {
      connector = connector.postgres
      query     = "SELECT 1 FROM entities WHERE id = :parent_id"
      params    = { parent_id = "input.headers.parent_id" }
      if_exists = "pass"
    }

    storage              = connector.redis
    timeout              = "60s"
    on_timeout           = "retry"
    max_retries          = 3
    max_concurrent_waits = 10
  }

  transform {
    output.id        = input.body.id
    output.type      = input.headers.type
    output.parent_id = input.headers.parent_id
    output.name      = input.body.name
  }

  to { connector.postgres = "entities" }
}
```

### flows/call_external.mycel

```hcl
flow "call_external" {
  from { connector.rabbitmq = "queue:requests" }

  semaphore {
    storage     = connector.redis
    key         = "'external_api'"
    max_permits = 10
    timeout     = "30s"
    lease       = "60s"
  }

  to { connector.external_api = "POST /process" }
}
```

### flows/daily_cleanup.mycel

```hcl
flow "daily_cleanup" {
  when = "0 3 * * *"

  lock {
    storage = connector.redis
    key     = "'job:daily_cleanup'"
    timeout = "1h"
    wait    = false
  }

  to {
    connector = "postgres"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}
```

---

## Dependencias

```go
// go.mod (nuevas)
require (
    github.com/robfig/cron/v3 v3.0.1
)

// Ya existentes
require (
    github.com/redis/go-redis/v9
    github.com/google/cel-go
)
```

---

## Checklist de Implementación

- [ ] **Parte 1: Headers MQ**
  - [ ] Modificar RabbitMQ consumer para estructura input.body/headers/properties
  - [ ] Modificar Kafka consumer para estructura input.body/headers/key/topic/etc
  - [ ] Actualizar tests de MQ
  - [ ] Actualizar ejemplos

- [ ] **Parte 2: Lock**
  - [ ] `internal/sync/lock.go` - Interface y config
  - [ ] `internal/sync/lock_redis.go`
  - [ ] `internal/sync/lock_memory.go`
  - [ ] `internal/sync/lock_test.go`
  - [ ] Parser HCL para lock block

- [ ] **Parte 2.5: Semaphore**
  - [ ] `internal/sync/semaphore.go` - Interface y config
  - [ ] `internal/sync/semaphore_redis.go`
  - [ ] `internal/sync/semaphore_memory.go`
  - [ ] `internal/sync/semaphore_test.go`
  - [ ] Parser HCL para semaphore block

- [ ] **Parte 3: Coordinate**
  - [ ] `internal/sync/coordinate.go` - Interface y config
  - [ ] `internal/sync/coordinate_hub.go` - Redis con pub/sub hub
  - [ ] `internal/sync/coordinate_memory.go` - Memory con cleanup
  - [ ] `internal/sync/coordinate_test.go`
  - [ ] Implementar preflight check
  - [ ] Implementar max_concurrent_waits
  - [ ] Parser HCL para coordinate block

- [ ] **Parte 4: Flow Triggers**
  - [ ] `internal/scheduler/scheduler.go`
  - [ ] Parser para `when` en flows
  - [ ] Integrar scheduler en runtime
  - [ ] Tests de cron/interval

- [ ] **Parte 5: Métricas**
  - [ ] `internal/sync/metrics.go`
  - [ ] Registrar métricas en Prometheus

- [ ] **Parte 6: Integración**
  - [ ] Modificar flow_registry.go
  - [ ] Tests de integración
  - [ ] Ejemplo completo

- [ ] **Documentación**
  - [ ] Actualizar README
  - [ ] Actualizar CHANGELOG
  - [ ] Actualizar docs/CONFIGURATION.md

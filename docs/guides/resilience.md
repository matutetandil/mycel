# Resilience and Failure Recovery

This guide answers a question that comes up often: **"What happens to a running Mycel service when something fails — a crash, a power cut, a network blip?"**

The short answer requires drawing one line clearly:

- **Availability** (who restarts the process when it dies) is **not** Mycel's job. It belongs to the platform underneath — Kubernetes, Docker, or systemd.
- **Durability and recovery** (whether in-flight data survives the failure) **is** Mycel's job, and it has a full set of primitives for it.

Confusing these two is the most common misunderstanding about what Mycel is.

## Mental Model: Mycel Is a Microservice, Not an Orchestrator

Mycel is a runtime that *produces* a microservice. It is not a platform that *supervises* microservices. The analogy is nginx: if you kill the nginx process, nginx does not bring itself back to life — systemd or Kubernetes does. Mycel is the same.

| Concern | Question | Who owns it |
|---------|----------|-------------|
| **Liveness / availability** | "My process died — who starts it again?" | The orchestrator: Kubernetes, Docker `--restart`, systemd |
| **Durability / recovery** | "Did I lose the data that was in flight when it died?" | **Mycel** — via broker semantics, retries, idempotency, sagas, locks |

So when someone asks *"if the power goes out, does Mycel break?"* — yes, the **process** stops, exactly like any microservice written in Go, NestJS, or anything else. The platform restarts it. The real question worth answering is the second one: **what happens to the data?**

### Availability is the platform's job

Mycel exposes the hooks the platform needs and stops there:

- A `/health` endpoint (liveness) and a `/ready` endpoint (readiness) for probes.
- A clean process exit on fatal errors, so the supervisor can restart it.
- Stateless process design — all durable state lives in brokers, databases, and the lock/cache store, never in Mycel's memory.

Configure restart behavior where it belongs:

```yaml
# Kubernetes — the platform restarts the pod and probes health
livenessProbe:
  httpGet: { path: /health, port: 3000 }
readinessProbe:
  httpGet: { path: /ready, port: 3000 }
replicas: 3   # horizontal redundancy so one pod dying doesn't take you down
```

```bash
# Docker
docker run --restart=always mdenda/mycel
```

```ini
# systemd
[Service]
Restart=always
```

Because Mycel keeps no durable state in memory, restarting the process is safe: it reconnects to its brokers and databases and resumes from where the durable state left off.

## What Survives a Failure: It Depends on How the Message Arrived

This is the crux. Whether in-flight work survives a crash depends entirely on **the source of the message** — specifically, whether something durable is holding it while Mycel processes it.

```
┌─────────────────────────────────────────────────────────────┐
│  Async source (broker holds the message)                     │
│  RabbitMQ / Kafka / MQTT  ──►  transform  ──►  DB             │
│  Crash mid-flight → message is NOT acked → broker redelivers  │
│  Recovery: AUTOMATIC                                          │
├─────────────────────────────────────────────────────────────┤
│  Sync source (only the client holds the message)            │
│  REST / gRPC / TCP req-reply  ──►  transform  ──►  DB         │
│  Crash mid-flight → no broker → nothing to redeliver          │
│  Recovery: DEPENDS ON THE CALLER RETRYING                    │
└─────────────────────────────────────────────────────────────┘
```

### Case 1 — Message-driven source (recovery is automatic)

When the source is a message broker, Mycel uses standard **at-least-once** delivery semantics. The broker holds the message until Mycel explicitly acknowledges it *after* the flow completes successfully.

```hcl
flow "ingest_order" {
  from {
    connector = "rabbit"
    operation = "consume"
    queue     = "orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}
```

Timeline of a power cut:

1. Mycel pulls a message off `orders`. The broker marks it *unacknowledged*, not *removed*.
2. Mycel transforms it and starts writing to the DB.
3. **Power cut.** The process dies before the ack.
4. The broker sees the channel drop without an ack → it **redelivers** the message.
5. Kubernetes restarts the pod, Mycel reconnects, and the message is processed again.

No data lost. This is why async ingestion is the resilient default for anything that matters.

> **At-least-once, not exactly-once.** Redelivery means a message *can* be processed twice (if the crash happened *after* the DB write but *before* the ack). That is what idempotency is for — see [below](#making-reprocessing-safe-idempotency).

### Case 2 — Synchronous source (you depend on the caller)

This is the exact case from the question: **"I receive a message over REST, I transform it, I send it to a DB, and the power cuts in the middle — do I depend on the sender doing a resend?"**

**Yes. You do.**

```hcl
flow "create_user" {
  from {
    connector  = "api"
    operation  = "POST /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
```

With a synchronous protocol (REST, gRPC, TCP request/reply), there is **no broker holding the request**. The only copy of that message lives in the HTTP client that sent it. If Mycel dies mid-flight:

- The client's connection drops or times out. It receives no `2xx`.
- There is nothing for Mycel to "resume" — when it restarts, it has no record that the request ever existed.
- **Recovery depends entirely on the caller retrying.**

This is not a Mycel limitation — it is the nature of synchronous request/response. The same is true of any REST service in any language. The contract is: *the caller owns the message until it gets a success response.*

There is also a subtler partial-failure window:

```
client sends POST ──► transform ──► DB write COMMITS ──► [power cut] ──► response never sent
```

Here the write **succeeded**, but the client never got the `2xx`, so it retries — and now you have a **duplicate row**. This is why a synchronous write path that you care about needs idempotency (next section).

### Summary: pick the right ingestion for the durability you need

| Ingestion | Holds the message during processing | Recovery on crash |
|-----------|-------------------------------------|-------------------|
| RabbitMQ / Kafka / MQTT consume | The broker | Automatic redelivery |
| REST / gRPC / TCP request-reply | Only the client | Caller must retry |
| CDC (database change stream) | Checkpoint / log position | Resumes from last checkpoint |

If you need a synchronous API but durable recovery, the standard pattern is to **decouple ingestion**: accept the REST request, enqueue it to a broker, return `202 Accepted`, and process from the queue. That converts Case 2 into Case 1.

```hcl
# Front door: accept and enqueue durably, return immediately
flow "accept_order" {
  from { connector = "api",    operation = "POST /orders" }
  to   { connector = "rabbit", target    = "orders" }
}

# Worker: durable, redelivered on crash
flow "process_order" {
  from { connector = "rabbit", operation = "consume", queue = "orders" }
  to   { connector = "db",     target    = "orders" }
}
```

Mycel's [`async` block](../reference/configuration.md#async-block) gives you the same shape with a built-in job store and a polling endpoint, if you'd rather not run a broker.

## Making Reprocessing Safe: Idempotency

Because at-least-once delivery and client retries both create duplicates, the antidote is **idempotency** — making "process this twice" produce the same result as "process this once".

```hcl
flow "charge_payment" {
  from { connector = "api", operation = "POST /payments" }
  to   { connector = "db",  target    = "payments" }

  idempotency {
    storage = "redis_cache"
    key     = "input.payment_id"   # caller-supplied stable key
    ttl     = "24h"
  }
}
```

If a request with the same `payment_id` arrives again (a retry after a crash, or a broker redelivery), Mycel returns the cached result **without re-executing the flow** — no double charge, no duplicate row.

Pair this with database-level constraints (unique keys, upserts) for defense in depth.

## The Rest of the Resilience Toolbox

Beyond ingestion semantics and idempotency, Mycel provides layered failure handling. Each has its own detailed coverage in the [Error Handling guide](error-handling.md); this is the map of how they relate to recovery.

| Primitive | What it protects against | Reference |
|-----------|--------------------------|-----------|
| **Retry + backoff** (`error_handling.retry`) | Transient downstream failures (a DB or API briefly unavailable) | [Error Handling](error-handling.md#flow-level-error-handling) |
| **Dead Letter Queue** (`fallback`, `dlq{}`) | Messages that fail after all retries — parked, never silently dropped | [Error Handling](error-handling.md#rabbitmq-dead-letter-queue) |
| **Circuit breaker** | A failing dependency dragging the whole service down | [Error Handling](error-handling.md#circuit-breaker) |
| **Idempotency** | Duplicate processing from retries / redelivery | [above](#making-reprocessing-safe-idempotency) |
| **Sagas + compensation** | Partial completion of a multi-step distributed transaction — rolls back completed steps | [Sagas](sagas.md) |
| **Distributed locks with TTL** | A process holding a lock dying and leaving it stuck forever — the TTL releases it automatically | [Synchronization](synchronization.md) |
| **CDC checkpoints** | Losing your place in a database change stream after a restart | [Real-Time](real-time.md) |
| **Connector profiles** | A primary backend being down — fail over to an alternate | [Error Handling](error-handling.md#connector-profiles) |

### Locks survive a crashing holder

Worth calling out because it directly addresses "what if the process dies mid-operation": Mycel's distributed locks carry a **TTL with heartbeat extension**. If the process holding a lock crashes, it stops extending the TTL, and the lock is released automatically once it expires — no manual cleanup, no deadlock waiting for a process that will never come back.

## A Sentence You Can Reuse

> Mycel is a microservice, not an orchestrator. If the process dies, the platform underneath restarts it (Kubernetes, Docker, systemd) — exactly like any microservice in any language. What Mycel guarantees is that **you don't lose in-flight data**: it uses standard broker semantics (ack / redelivery), retries, dead letter queues, idempotency, sagas, and locks with TTL. The process is ephemeral; the state is durable. The one case where recovery depends on the caller is a purely synchronous request (REST/gRPC) with no broker in front — there, the client owns the message until it gets a success response.

## A Note on Certification

Resilience features are tools the framework gives you. They do **not** make a deployed service automatically compliant with any standard (PCI, SOC 2, etc.) — those are certifications of *your service*, not of Mycel the framework. Mycel provides the building blocks; the system you assemble with them is what gets evaluated. See [Security](security.md) for the same distinction applied to security controls.

## See Also

- [Error Handling](error-handling.md) — Retry, DLQ, circuit breakers, on-error aspects (the detailed reference for most primitives above)
- [Sagas & State Machines](sagas.md) — Distributed transactions with automatic compensation
- [Synchronization](synchronization.md) — Distributed locks, semaphores, and lock TTLs
- [Observability](observability.md) — Metrics and health checks to detect failures
- [Production Guide](../deployment/production.md) — Deployment hardening
- [Kubernetes Deployment](../deployment/kubernetes.md) — Probes, replicas, restart policy

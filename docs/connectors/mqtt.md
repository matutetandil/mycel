# MQTT

Lightweight publish/subscribe messaging for IoT, sensor networks, and real-time telemetry. Supports QoS levels 0–2, topic wildcards (`+`, `#`), TLS, and automatic reconnection with re-subscription. Use it for device-to-cloud ingestion, command dispatch, or any scenario where bandwidth-efficient push messaging matters.

## Configuration

```hcl
connector "sensors" {
  type      = "mqtt"
  broker    = "tcp://localhost:1883"
  client_id = "mycel-iot-gateway"
  qos       = 1
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `broker` | string | `"tcp://localhost:1883"` | Broker URL (`tcp://`, `ssl://`, `ws://`) |
| `client_id` | string | `"mycel"` | Client identifier |
| `username` | string | — | Authentication username |
| `password` | string | — | Authentication password |
| `qos` | int | `0` | Default QoS level (0 = at most once, 1 = at least once, 2 = exactly once) |
| `topic` | string | — | Default publish topic |
| `clean_session` | bool | `true` | Whether to start a clean session on connect |
| `keep_alive` | duration | `"30s"` | PINGREQ interval |
| `connect_timeout` | duration | `"10s"` | Connection timeout |
| `auto_reconnect` | bool | `true` | Reconnect automatically on disconnect |
| `max_reconnect_interval` | duration | `"5m"` | Maximum wait between reconnection attempts |

### TLS

```hcl
connector "sensors" {
  type      = "mqtt"
  broker    = "ssl://broker.example.com:8883"
  client_id = "mycel-secure"

  tls {
    enabled  = true
    cert     = "/certs/client.crt"
    key      = "/certs/client.key"
    ca       = "/certs/ca.crt"
    insecure = false
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `tls.enabled` | bool | `false` | Enable TLS |
| `tls.cert` | string | — | Client certificate file |
| `tls.key` | string | — | Client private key file |
| `tls.ca` | string | — | CA certificate file |
| `tls.insecure` | bool | `false` | Skip server certificate verification |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `<topic>` | source | Subscribe to a topic (supports MQTT wildcards `+`, `#`) |
| `<topic>` | target | Publish a message to a topic |

### Source (Subscribe)

When used as a flow source, the operation is the MQTT topic filter to subscribe to. MQTT wildcards are supported:

- `sensors/+/temperature` — matches `sensors/room1/temperature`, `sensors/lab/temperature`
- `devices/#` — matches `devices/status`, `devices/room1/temperature`, etc.

Incoming messages provide these fields:

| Field | Description |
|-------|-------------|
| `_topic` | The actual topic the message was published on |
| `_message_id` | Broker-assigned message ID |
| `_qos` | QoS level of the received message |
| `_retained` | Whether the message was a retained message |
| `*` | All JSON fields from the payload are merged at the top level |
| `_raw` | Raw string payload (only if payload is not valid JSON) |

### Target (Publish)

When used as a flow target, the `target` field is the topic to publish to:

```hcl
to {
  connector = "sensors"
  target    = "alerts/temperature"
}
```

Optional params: `qos` (int), `retain` (bool).

## Example

```hcl
# Subscribe to sensor readings and store in database
flow "sensor_reading" {
  from {
    connector = "sensors"
    operation = "sensors/+/temperature"
  }
  transform {
    device_id   = "input._topic"
    value       = "input.temperature"
    unit        = "'celsius'"
    received_at = "now()"
  }
  to {
    connector = "db"
    target    = "readings"
  }
}

# Publish alerts via REST
flow "temperature_alert" {
  from {
    connector = "api"
    operation = "POST /alerts"
  }
  to {
    connector = "sensors"
    target    = "alerts/temperature"
  }
}
```

See the [mqtt example](../../examples/mqtt/) for a complete working setup.

---

> **Full configuration reference:** See [MQTT](../reference/configuration.md#mqtt) in the Configuration Reference.

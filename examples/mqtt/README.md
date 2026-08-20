# MQTT

An IoT gateway: sensor readings arrive over MQTT and are stored, and an alert
posted over HTTP is published back to the broker.

## Files

| File | Purpose |
|------|---------|
| `config.mycel` | Connectors and flows |
| `migrations/001_create_readings.sql` | The table the readings are stored in |

## What it does

```
MQTT sensors/+/temperature → flow "sensor_reading"    → sqlite readings
POST /alerts               → flow "temperature_alert" → MQTT alerts/temperature
```

`+` matches one level of a topic, so `sensors/kitchen/temperature` and
`sensors/attic/temperature` both reach the same flow; `input._topic` says which
one it was.

## Running

A broker is not part of the example. Any MQTT broker will do — Mosquitto is one
line of Docker:

```bash
docker run -d --rm -p 1883:1883 eclipse-mosquitto:2 \
  mosquitto -c /mosquitto-no-auth.conf

export MQTT_BROKER=tcp://localhost:1883

mycel migrate --config ./examples/mqtt
mycel start --config ./examples/mqtt
```

## Try It

Publish a reading and it is stored:

```bash
mosquitto_pub -t 'sensors/kitchen/temperature' -m '{"temperature":21.5}'
```

Post an alert and it goes out on the broker — subscribe first to watch it
arrive:

```bash
mosquitto_sub -t 'alerts/#' &

curl -X POST http://localhost:3000/alerts \
  -H "Content-Type: application/json" \
  -d '{"device":"kitchen","reading":31.2,"threshold":30}'
```

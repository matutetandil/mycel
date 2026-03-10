# Scheduled Jobs Example

Demonstrates scheduled (cron) flows running alongside a REST API in a single Mycel service.

## What This Example Does

- Runs three scheduled jobs on different schedules
- Exposes a REST API on port 3000 to query job results
- Stores all data in a local SQLite database
- Shows that scheduled flows and API flows coexist naturally

### Scheduled Jobs

| Flow | Schedule | Description |
|------|----------|-------------|
| `cleanup_old_logs` | Daily at 3:00 AM | Deletes log entries older than 30 days |
| `health_ping` | Every 5 minutes | Inserts a heartbeat record |
| `weekly_report` | Mondays at 9:00 AM | Queries log stats and writes a report row |

## Quick Start

```bash
# From the repository root
mycel start --config ./examples/scheduled

# Or with Docker
docker run -v $(pwd)/examples/scheduled:/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

## Verify It Works

### 1. Wait for a heartbeat (up to 5 minutes)

```bash
curl http://localhost:3000/heartbeats
```

Expected response:
```json
[{"id":1,"service":"scheduled-jobs-service","status":"alive","created_at":"2026-03-09T12:00:00Z"}]
```

### 2. Check reports (after Monday 9 AM)

```bash
curl http://localhost:3000/reports
```

### 3. Check logs

```bash
curl http://localhost:3000/logs
```

## File Structure

```
scheduled/
├── config.hcl              # Service name and version
├── connectors/
│   ├── api.hcl             # REST API on port 3000
│   └── database.hcl        # SQLite database connection
├── flows/
│   ├── scheduled.hcl       # Three scheduled jobs (cron flows)
│   └── api.hcl             # REST endpoints to query results
└── data/
    └── app.db              # SQLite database file (created automatically)
```

## Configuration Explained

### Scheduled Flows (`flows/scheduled.hcl`)

The `when` attribute turns a flow into a scheduled job. No `from` block is needed -- the scheduler triggers the flow automatically.

**Cron expression** -- standard 5-field format (minute hour day-of-month month day-of-week):

```hcl
flow "cleanup_old_logs" {
  when = "0 3 * * *"    # 3:00 AM every day

  to {
    connector = "sqlite"
    target    = "logs"
    operation = "DELETE"
    filter    = "created_at < datetime('now', '-30 days')"
  }
}
```

**Interval shorthand** -- runs on a fixed interval:

```hcl
flow "health_ping" {
  when = "@every 5m"    # Every 5 minutes

  transform {
    output.service = "scheduled-jobs-service"
    output.status  = "alive"
  }

  to {
    connector = "sqlite"
    target    = "heartbeats"
  }
}
```

**Steps in scheduled flows** -- use `step` blocks for multi-stage jobs:

```hcl
flow "weekly_report" {
  when = "0 9 * * 1"   # Monday at 9:00 AM

  step "get_stats" {
    connector = "sqlite"
    target    = "logs"
    operation = "SELECT count(*) as total_logs, ..."
  }

  to {
    connector = "sqlite"
    target    = "reports"
  }
}
```

### Supported Schedule Formats

| Format | Example | Description |
|--------|---------|-------------|
| Cron (5-field) | `"0 3 * * *"` | min hour dom month dow |
| `@hourly` | `"@hourly"` | Every hour at minute 0 |
| `@daily` | `"@daily"` | Every day at midnight |
| `@weekly` | `"@weekly"` | Every Sunday at midnight |
| `@monthly` | `"@monthly"` | First day of month at midnight |
| `@yearly` | `"@yearly"` | January 1st at midnight |
| Interval | `"@every 5m"` | Fixed interval (s, m, h) |

## What You Should See in Logs

When the service starts:
```
INFO  Starting service: scheduled-jobs-service
INFO  Loaded 2 connectors: api, sqlite
INFO  Registered 6 flows: cleanup_old_logs, health_ping, weekly_report, get_logs, get_heartbeats, get_reports
INFO  Scheduled: cleanup_old_logs (0 3 * * *)
INFO  Scheduled: health_ping (@every 5m)
INFO  Scheduled: weekly_report (0 9 * * 1)
INFO  REST server listening on :3000
```

Every 5 minutes:
```
INFO  Scheduled flow triggered: health_ping
INFO  health_ping → sqlite:heartbeats (INSERT)
```

## Next Steps

- Add error handling with retries: See [docs/ERROR_HANDLING.md](../../docs/ERROR_HANDLING.md)
- Send notifications on failure: See [examples/notifications](../notifications)
- Add transforms to enrich data: See [docs/CONCEPTS.md](../../docs/CONCEPTS.md#transforms)

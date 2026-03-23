# Scheduled flows configuration

# Delete logs older than 30 days - runs daily at 3:00 AM
flow "cleanup_old_logs" {
  when = "0 3 * * *"

  to {
    connector = "sqlite"
    target    = "logs"
    operation = "DELETE"
    filter    = "created_at < datetime('now', '-30 days')"
  }
}

# Insert a heartbeat record - runs every 5 minutes
flow "health_ping" {
  when = "@every 5m"

  transform {
    output.service = "scheduled-jobs-service"
    output.status  = "alive"
  }

  to {
    connector = "sqlite"
    target    = "heartbeats"
  }
}

# Generate weekly stats report - runs every Monday at 9:00 AM
flow "weekly_report" {
  when = "0 9 * * 1"

  step "get_stats" {
    connector = "sqlite"
    target    = "logs"
    operation = "SELECT count(*) as total_logs, date('now', '-7 days') as period_start, date('now') as period_end"
  }

  transform {
    output.total_logs   = input.total_logs
    output.period_start = input.period_start
    output.period_end   = input.period_end
    output.generated_at = now()
  }

  to {
    connector = "sqlite"
    target    = "reports"
  }
}

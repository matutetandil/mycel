# Flow Triggers Example - Scheduled Jobs
# Run jobs on cron or interval schedules
# NOTE: Scheduled triggers (when attribute) are a planned feature

# Daily cleanup
flow "daily_cleanup" {
  from {
    connector = "api"
    operation = "POST /jobs/cleanup"
  }

  # Prevent concurrent runs with a lock
  lock {
    storage = "redis"
    key     = "'job:daily_cleanup'"
    timeout = "1h"
    wait    = false
  }

  to {
    connector = "postgres"
    operation = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

# Health check endpoint
flow "health_ping" {
  from {
    connector = "api"
    operation = "GET /health"
  }

  to {
    connector = "monitoring"
    operation = "POST /post"
  }
}

# Manual stats aggregation trigger
flow "aggregate_stats" {
  from {
    connector = "api"
    operation = "POST /jobs/stats"
  }

  lock {
    storage = "redis"
    key     = "'job:hourly_stats'"
    timeout = "30m"
    wait    = false
  }

  to {
    connector = "postgres"
    operation = "INSERT INTO hourly_stats (hour, total_requests) VALUES (now(), 0)"
  }
}

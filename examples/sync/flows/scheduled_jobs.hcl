# Flow Triggers Example - Scheduled Jobs
# Run jobs on cron or interval schedules

# Daily cleanup at 3 AM
flow "daily_cleanup" {
  when = "0 3 * * *"  # Cron: minute hour day month weekday

  # Prevent concurrent runs with a lock
  lock {
    storage = "redis"
    key     = "'job:daily_cleanup'"
    timeout = "1h"
    wait    = false  # Skip if already running
  }

  to {
    connector = "postgres"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

# Health check every 5 minutes
flow "health_ping" {
  when = "@every 5m"

  to {
    connector = "monitoring"
    target    = "POST /post"
  }
}

# Hourly stats aggregation
flow "hourly_stats" {
  when = "@hourly"  # Shortcut for "0 * * * *"

  lock {
    storage = "redis"
    key     = "'job:hourly_stats'"
    timeout = "30m"
    wait    = false
  }

  to {
    connector = "postgres"
    query     = <<-SQL
      INSERT INTO hourly_stats (hour, total_requests, avg_response_time)
      SELECT
        date_trunc('hour', created_at) as hour,
        count(*) as total_requests,
        avg(response_time) as avg_response_time
      FROM request_logs
      WHERE created_at >= now() - interval '1 hour'
      GROUP BY date_trunc('hour', created_at)
      ON CONFLICT (hour) DO UPDATE SET
        total_requests = EXCLUDED.total_requests,
        avg_response_time = EXCLUDED.avg_response_time
    SQL
  }
}

# Weekly report - every Monday at 9 AM
flow "weekly_report" {
  when = "0 9 * * 1"  # Monday at 9:00 AM

  to {
    connector = "postgres"
    query     = <<-SQL
      INSERT INTO weekly_reports (week_start, summary)
      SELECT
        date_trunc('week', now()) as week_start,
        jsonb_build_object(
          'total_users', (SELECT count(*) FROM users WHERE created_at >= now() - interval '7 days'),
          'total_orders', (SELECT count(*) FROM orders WHERE created_at >= now() - interval '7 days')
        ) as summary
    SQL
  }
}

# API flows configuration

# GET /logs - List recent log entries
flow "get_logs" {
  from {
    connector = "api"
    operation = "GET /logs"
  }

  to {
    connector = "sqlite"
    target    = "logs"
  }
}

# GET /heartbeats - List heartbeat records
flow "get_heartbeats" {
  from {
    connector = "api"
    operation = "GET /heartbeats"
  }

  to {
    connector = "sqlite"
    target    = "heartbeats"
  }
}

# GET /reports - List generated reports
flow "get_reports" {
  from {
    connector = "api"
    operation = "GET /reports"
  }

  to {
    connector = "sqlite"
    target    = "reports"
  }
}

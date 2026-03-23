# Flow: Execute local command and return result via API
flow "get_system_info" {
  from {
    connector = "api"
    operation = "GET /system/info"
  }

  to {
    connector = "system_info"
    target    = ""
  }
}

# Flow: Get disk usage via shell command
flow "get_disk_usage" {
  from {
    connector = "api"
    operation = "GET /system/disk"
  }

  to {
    connector = "disk_usage"
    target    = ""
  }
}

# Flow: Echo back a message (using exec as target)
flow "echo_message" {
  from {
    connector = "api"
    operation = "POST /echo"
  }

  to {
    connector = "local_exec"
    target    = ""
  }
}

# Flow: Enrich data using an exec connector (fetch external data)
# This demonstrates using exec for enrichment
flow "get_enriched_data" {
  from {
    connector = "api"
    operation = "GET /enriched/:id"
  }

  # Fetch additional data by executing a command
  enrich "external_data" {
    connector = "json_script"
    operation = ""
    params {
      id = "input.id"
    }
  }

  transform {
    id        = "input.id"
    status    = "enriched.external_data.status"
    timestamp = "enriched.external_data.timestamp"
  }

  to {
    connector = "api"
    target    = ""
  }
}

# Accept block flows

# Flow with accept gate — only processes requests where region is "us-east"
flow "accept_gate" {
  from {
    connector = "api"
    operation = "POST /test/accept"
    filter    = "has(input.action)"
  }

  accept {
    when      = "input.region == 'us-east'"
    on_reject = "ack"
  }

  transform {
    action = "input.action"
    region = "input.region"
  }

  to {
    connector = "postgres"
    target    = "accept_results"
  }
}

# Read back accept results
flow "get_accept_results" {
  from {
    connector = "api"
    operation = "GET /test/accept/results"
  }
  to {
    connector = "postgres"
    target    = "accept_results"
  }
}

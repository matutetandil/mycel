# Filter flows

flow "filter_pass" {
  from {
    connector = "api"
    operation = "POST /test/filter"
    filter    = "input.status == 'active'"
  }

  transform {
    result = "'passed'"
    status = "input.status"
  }

  to {
    connector = "postgres"
    target    = "filter_results"
  }
}

# Read back filter results
flow "get_filter_results" {
  from {
    connector = "api"
    operation = "GET /test/filter/results"
  }
  to {
    connector = "postgres"
    target    = "filter_results"
  }
}

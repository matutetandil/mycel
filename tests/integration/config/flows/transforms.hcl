# Transform/CEL function tests

flow "test_transforms" {
  from {
    connector = "api"
    operation = "POST /test/transforms"
  }

  transform {
    generated_id = "uuid()"
    lowered      = "lower(input.text)"
    uppered      = "upper(input.text)"
    timestamp    = "now()"
    combined     = "input.first + ' ' + input.last"
  }

  to {
    connector = "postgres"
    target    = "transform_results"
  }
}

# Read back transform results to verify
flow "get_transform_results" {
  from {
    connector = "api"
    operation = "GET /test/transforms/results"
  }
  to {
    connector = "postgres"
    target    = "transform_results"
  }
}

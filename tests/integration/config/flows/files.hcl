# File flows
# File connector uses Call() interface for operations.
# POST flows with step results can't write maps to postgres, so
# we use GET for read and accept POST write result is just ack.

flow "file_write" {
  from {
    connector = "api"
    operation = "POST /files/write"
  }
  step "write" {
    connector = "files"
    operation = "write"
    params = {
      path    = "input.filename"
      content = "input.content"
    }
  }
  transform {
    data = "'written'"
  }
  to {
    connector = "postgres"
    target    = "step_results"
  }
}

flow "file_read" {
  from {
    connector = "api"
    operation = "GET /files/read/:filename"
  }
  step "read" {
    connector = "files"
    operation = "read"
    params = {
      path = "input.filename"
    }
  }
  transform {
    content = "step.read"
  }
  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "postgres"
    target    = "step_results"
  }
}

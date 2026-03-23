# S3 flows (MinIO)
# S3 connector's Call() supports: list, delete, exists, head, copy, move
# S3 Write/Read have non-standard signatures and don't implement connector.Writer/Reader
# So we can only test operations available through Call()

flow "s3_list" {
  from {
    connector = "api"
    operation = "GET /s3/files"
  }
  step "listing" {
    connector = "s3"
    operation = "list"
  }
  transform {
    files = "step.listing"
  }
  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "postgres"
    target    = "step_results"
  }
}

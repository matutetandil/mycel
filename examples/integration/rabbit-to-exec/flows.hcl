# Flows: RabbitMQ -> Exec
# NOTE: Some advanced features (inline connector config) are planned

# Pattern 1: Simple script execution
flow "generate_pdf" {
  from {
    connector = "rabbit"
    operation = "documents.pdf_generation"
  }

  to {
    connector = "exec"
    operation = "generate_pdf"
  }
}

# Pattern 2: Python script execution
flow "process_data" {
  from {
    connector = "rabbit"
    operation = "data.processing"
  }

  to {
    connector = "exec_python"
    operation = "process_data"
  }
}

# Pattern 3: Image processing with semaphore
flow "process_image" {
  semaphore {
    storage     = "memory"
    key         = "'image_processing'"
    max_permits = 3
    timeout     = "5m"
  }

  from {
    connector = "rabbit"
    operation = "images.processing"
  }

  to {
    connector = "exec"
    operation = "process_image"
  }
}

# Pattern 4: Video transcoding with lock
flow "transcode_video" {
  lock {
    storage = "memory"
    key     = "'video:' + input.body.video_id"
    timeout = "30m"
    wait    = false
  }

  from {
    connector = "rabbit"
    operation = "videos.transcoding"
  }

  to {
    connector = "exec"
    operation = "transcode"
  }
}

# Pattern 5: ETL pipeline execution
flow "run_etl_pipeline" {
  from {
    connector = "rabbit"
    operation = "etl.jobs"
  }

  to {
    connector = "exec"
    operation = "run_pipeline"
  }
}

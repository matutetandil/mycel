# Flows: RabbitMQ -> Exec

# Pattern 1: Simple script execution
flow "generate_pdf" {
  description = "Consume PDF generation requests and run script"

  from {
    connector.rabbit = {
      queue   = "documents.pdf_generation"
      durable = true

      bind {
        exchange    = "documents"
        routing_key = "pdf.generate"
      }

      auto_ack = false
      format   = "json"

      dlq {
        enabled     = true
        queue       = "documents.pdf_generation.dlq"
        max_retries = 2
      }
    }
  }

  to {
    connector.exec = {
      command = "./generate_pdf.sh"
      args    = [
        "${input.body.template_id}",
        "${input.body.output_path}",
        "${json(input.body.data)}"
      ]
      timeout = "2m"
    }
  }
}

# Pattern 2: Python script with JSON input
flow "process_data" {
  description = "Run Python data processing script"

  from {
    connector.rabbit = {
      queue   = "data.processing"
      durable = true

      bind {
        exchange    = "data"
        routing_key = "data.process"
      }

      format = "json"

      dlq {
        enabled     = true
        queue       = "data.processing.dlq"
        max_retries = 3
      }
    }
  }

  to {
    connector.exec_python = {
      command = "python3"
      args    = [
        "process_data.py",
        "--input-file", "${input.body.input_file}",
        "--output-file", "${input.body.output_file}",
        "--config", "${json(input.body.config)}"
      ]
      timeout = "10m"
    }
  }
}

# Pattern 3: Image processing with semaphore (limit concurrent processing)
flow "process_image" {
  description = "Process images with concurrency limit"

  semaphore {
    key          = "image_processing"
    permits      = 3  # Max 3 concurrent image processing jobs
    storage      = "memory"
    timeout      = "5m"
    on_fail      = "wait"
    wait_timeout = "10m"
  }

  from {
    connector.rabbit = {
      queue   = "images.processing"
      durable = true

      bind {
        exchange    = "images"
        routing_key = "image.*"
      }

      format = "json"
    }
  }

  to {
    connector.exec = {
      command = "./process_image.sh"
      args    = [
        "${input.body.source_path}",
        "${input.body.dest_path}",
        "${input.body.operation}",  # resize, crop, watermark, etc.
        "${input.body.params}"
      ]
      timeout = "3m"
    }
  }
}

# Pattern 4: Video transcoding (long-running)
flow "transcode_video" {
  description = "Transcode video files"

  lock {
    key     = "'video:' + input.body.video_id"
    storage = "memory"
    timeout = "30m"
    on_fail = "skip"  # Skip if same video is already being processed
  }

  from {
    connector.rabbit = {
      queue   = "videos.transcoding"
      durable = true

      bind {
        exchange    = "videos"
        routing_key = "video.transcode"
      }

      format = "json"

      dlq {
        enabled     = true
        queue       = "videos.transcoding.dlq"
        max_retries = 1  # Video transcoding is expensive, limit retries
      }
    }
  }

  to {
    connector.exec = {
      command = "./transcode.sh"
      args    = [
        "${input.body.input_path}",
        "${input.body.output_path}",
        "${input.body.format}",      # mp4, webm, etc.
        "${input.body.resolution}",  # 1080p, 720p, etc.
        "${input.body.bitrate}"
      ]
      timeout = "30m"
    }
  }
}

# Pattern 5: Shell pipeline execution
flow "run_etl_pipeline" {
  description = "Execute ETL pipeline script"

  from {
    connector.rabbit = {
      queue   = "etl.jobs"
      durable = true

      bind {
        exchange    = "etl"
        routing_key = "job.run"
      }

      format = "json"
    }
  }

  to {
    connector.exec = {
      command = "bash"
      args    = [
        "-c",
        "cd /app/etl && ./run_pipeline.sh ${input.body.pipeline_name} --date=${input.body.date} --env=${input.body.environment} 2>&1 | tee /var/log/etl/${input.body.pipeline_name}.log"
      ]
      shell   = true
      timeout = "1h"
    }
  }
}

# Pattern 6: Execute with input via stdin
flow "process_json_stream" {
  description = "Process JSON data via stdin to script"

  from {
    connector.rabbit = {
      queue   = "streams.json"
      durable = true
      format  = "json"
    }
  }

  to {
    connector.exec = {
      command = "python3"
      args    = ["json_processor.py"]
      stdin   = "${json(input.body)}"
      timeout = "5m"
    }
  }
}

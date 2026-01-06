# Flows: File -> RabbitMQ

# Pattern 1: Scheduled file processing (cron)
flow "process_daily_import" {
  description = "Process daily CSV import files"

  when = "0 6 * * *"  # Every day at 6am

  from {
    connector.files = {
      path   = "imports/daily/*.csv"
      format = "csv"

      csv {
        delimiter  = ","
        header     = true
        skip_empty = true
      }

      # Process files matching pattern
      glob = true

      # Move processed files to archive
      on_success {
        move_to = "imports/archive/${date('YYYY-MM-DD')}/"
      }

      on_error {
        move_to = "imports/failed/"
      }
    }
  }

  # Each row becomes a message
  foreach "row" in "input.rows" {
    transform {
      output.record_id  = "row.id ?? uuid()"
      output.data       = "row"
      output.source     = "input.file_name"
      output.imported_at = "now()"
    }

    to {
      connector.rabbit = {
        exchange    = "imports"
        routing_key = "import.daily.record"
        persistent  = true

        headers {
          "x-source-file" = "${input.file_name}"
          "x-record-id"   = "${output.record_id}"
        }

        format = "json"
      }
    }
  }
}

# Pattern 2: File watcher (interval-based polling)
flow "watch_drop_folder" {
  description = "Watch for new files in drop folder"

  when = "@every 30s"  # Check every 30 seconds

  from {
    connector.files = {
      path = "dropbox/"
      glob = "*.json"

      # Only process new files (not already processed)
      filter {
        newer_than = "30s"  # Files created in last 30s
      }

      format = "json"
    }
  }

  transform {
    output.file_id    = "uuid()"
    output.file_name  = "input.file_name"
    output.file_path  = "input.file_path"
    output.content    = "input.body"
    output.size       = "input.size"
    output.created_at = "input.created_at"
    output.processed_at = "now()"
  }

  to {
    connector.rabbit = {
      exchange    = "imports"
      routing_key = "file.dropped"
      persistent  = true

      headers {
        "x-file-id"   = "${output.file_id}"
        "x-file-name" = "${output.file_name}"
        "x-file-size" = "${output.size}"
      }

      format = "json"
    }
  }

  # Mark file as processed
  after {
    connector.files = {
      move = "${input.file_path}"
      to   = "dropbox/processed/${input.file_name}"
    }
  }
}

# Pattern 3: S3 file processing
flow "process_s3_uploads" {
  description = "Process files uploaded to S3"

  when = "@every 1m"

  from {
    connector.s3 = {
      prefix = "uploads/"
      format = "json"

      # List files modified since last run
      filter {
        modified_since = "last_run"
      }
    }
  }

  transform {
    output.object_key  = "input.key"
    output.bucket      = "input.bucket"
    output.content     = "input.body"
    output.etag        = "input.etag"
    output.size        = "input.size"
    output.uploaded_at = "input.last_modified"
  }

  to {
    connector.rabbit = {
      exchange    = "imports"
      routing_key = "s3.uploaded"
      persistent  = true

      headers {
        "x-s3-bucket" = "${output.bucket}"
        "x-s3-key"    = "${output.object_key}"
        "x-s3-etag"   = "${output.etag}"
      }

      format = "json"
    }
  }
}

# Pattern 4: Bulk file ingestion (all records in one message)
flow "ingest_price_list" {
  description = "Ingest complete price list file as single message"

  when = "0 5 * * 1"  # Every Monday at 5am

  from {
    connector.files = {
      path   = "imports/price_list.csv"
      format = "csv"

      csv {
        delimiter = ";"
        header    = true
      }
    }
  }

  transform {
    output.price_list_id = "uuid()"
    output.effective_date = "now()"
    output.items          = "input.rows"
    output.item_count     = "size(input.rows)"
    output.source_file    = "input.file_name"
  }

  to {
    connector.rabbit = {
      exchange    = "imports"
      routing_key = "import.pricelist"
      persistent  = true

      headers {
        "x-price-list-id" = "${output.price_list_id}"
        "x-item-count"    = "${output.item_count}"
      }

      format = "json"
    }
  }
}

# Pattern 5: XML file processing
flow "process_xml_feed" {
  description = "Process XML data feed files"

  when = "@every 15m"

  from {
    connector.files = {
      path   = "feeds/*.xml"
      glob   = true
      format = "xml"

      xml {
        root_element = "items"
        item_element = "item"
      }

      on_success {
        move_to = "feeds/processed/"
      }
    }
  }

  foreach "item" in "input.items" {
    transform {
      output.item_id    = "item.id"
      output.name       = "item.name"
      output.attributes = "item.attributes"
      output.feed_file  = "input.file_name"
      output.parsed_at  = "now()"
    }

    to {
      connector.rabbit = {
        exchange    = "imports"
        routing_key = "feed.item"
        persistent  = true

        format = "json"
      }
    }
  }
}

# Pattern 6: Line-by-line log processing
flow "stream_logs" {
  description = "Stream log file lines to queue"

  when = "@every 10s"

  from {
    connector.files = {
      path   = "logs/app.log"
      format = "lines"

      # Tail mode - only read new lines since last run
      tail = true

      # Remember position between runs
      state_file = ".mycel/log_positions.json"
    }
  }

  foreach "line" in "input.lines" {
    # Only process lines that match pattern
    when = "matches(line, '.*ERROR.*') || matches(line, '.*WARN.*')"

    transform {
      output.log_line   = "line"
      output.level      = "matches(line, '.*ERROR.*') ? 'error' : 'warn'"
      output.source     = "input.file_name"
      output.timestamp  = "now()"
    }

    to {
      connector.rabbit = {
        exchange    = "imports"
        routing_key = "'log.' + output.level"
        persistent  = false  # Logs don't need persistence

        format = "json"
      }
    }
  }
}

# Pattern 7: Multi-file batch processing with coordination
flow "process_order_batch" {
  description = "Process order files with header/detail pattern"

  when = "0 */2 * * *"  # Every 2 hours

  # Process header file first
  from {
    connector.files = {
      path   = "orders/batch_*.header.csv"
      glob   = true
      format = "csv"
    }
  }

  transform {
    output.batch_id     = "replace(input.file_name, '.header.csv', '')"
    output.header       = "input.rows[0]"
    output.order_count  = "input.rows[0].order_count"
    output.total_amount = "input.rows[0].total_amount"
  }

  # Signal that header is processed
  signal {
    key     = "'batch:' + output.batch_id"
    storage = "memory"
  }

  to {
    connector.rabbit = {
      exchange    = "imports"
      routing_key = "order.batch.header"
      persistent  = true

      headers {
        "x-batch-id" = "${output.batch_id}"
      }

      format = "json"
    }
  }
}

flow "process_order_details" {
  description = "Process order detail files after header"

  when = "0 */2 * * *"  # Same schedule as header

  # Wait for header to be processed
  wait {
    key          = "'batch:' + replace(input.file_name, '.detail.csv', '')"
    storage      = "memory"
    timeout      = "5m"
    on_timeout   = "skip"
  }

  from {
    connector.files = {
      path   = "orders/batch_*.detail.csv"
      glob   = true
      format = "csv"
    }
  }

  foreach "row" in "input.rows" {
    transform {
      output.batch_id  = "replace(input.file_name, '.detail.csv', '')"
      output.order_id  = "row.order_id"
      output.customer  = "row.customer_id"
      output.items     = "row.items"
      output.total     = "row.total"
    }

    to {
      connector.rabbit = {
        exchange    = "imports"
        routing_key = "order.batch.detail"
        persistent  = true

        headers {
          "x-batch-id"  = "${output.batch_id}"
          "x-order-id"  = "${output.order_id}"
        }

        format = "json"
      }
    }
  }
}

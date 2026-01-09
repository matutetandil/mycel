# Flows: File -> RabbitMQ
# NOTE: Some advanced features (cron scheduling, foreach, inline file config) are planned

# Pattern 1: Scheduled file processing
flow "process_daily_import" {
  from {
    connector = "files"
    operation = "imports/daily/*.csv"
  }

  transform {
    record_id   = "uuid()"
    data        = "input.row"
    source      = "input.file_name"
    imported_at = "now()"
  }

  to {
    connector = "rabbit"
    operation = "import.daily.record"
  }
}

# Pattern 2: File watcher
flow "watch_drop_folder" {
  from {
    connector = "files"
    operation = "dropbox/*.json"
  }

  transform {
    file_id      = "uuid()"
    file_name    = "input.file_name"
    file_path    = "input.file_path"
    content      = "input.body"
    processed_at = "now()"
  }

  to {
    connector = "rabbit"
    operation = "file.dropped"
  }
}

# Pattern 3: S3 file processing
flow "process_s3_uploads" {
  from {
    connector = "s3"
    operation = "uploads/*"
  }

  transform {
    object_key  = "input.key"
    bucket      = "input.bucket"
    content     = "input.body"
    uploaded_at = "input.last_modified"
  }

  to {
    connector = "rabbit"
    operation = "s3.uploaded"
  }
}

# Pattern 4: Bulk file ingestion
flow "ingest_price_list" {
  from {
    connector = "files"
    operation = "imports/price_list.csv"
  }

  transform {
    price_list_id  = "uuid()"
    effective_date = "now()"
    items          = "input.rows"
    item_count     = "size(input.rows)"
    source_file    = "input.file_name"
  }

  to {
    connector = "rabbit"
    operation = "import.pricelist"
  }
}

# S3 File Operations Flows

# Upload to S3
flow "upload_to_s3" {
  from {
    connector = "api"
    operation = "POST /upload"
  }

  transform {
    id         = "uuid()"
    key        = "'uploads/' + uuid() + '/' + input.filename"
    filename   = "input.filename"
    size       = "input.size"
    mime_type  = "input.content_type"
    created_at = "now()"
  }

  to {
    connector = "s3"
    operation = "WRITE"
    target    = "input.key"
  }

  # Save metadata
  after {
    to {
      connector = "db"
      target    = "files"
      operation = "INSERT"
    }
  }
}

# Download from S3
flow "download_from_s3" {
  from {
    connector = "api"
    operation = "GET /download/:id"
  }

  # Get metadata
  to {
    connector = "db"
    target    = "files"
  }

  # Get file content from S3
  enrich "file" {
    connector = "s3"
    operation = "READ"
    params {
      key = "output.key"
    }
  }

  transform {
    filename   = "output.filename"
    content    = "enriched.file.data"
    mime_type  = "output.mime_type"
  }
}

# Generate presigned URL for direct download
flow "get_presigned_url" {
  from {
    connector = "api"
    operation = "GET /presigned/:id"
  }

  # Get file metadata
  to {
    connector = "db"
    target    = "files"
  }

  # Generate presigned URL
  enrich "url" {
    connector = "s3"
    operation = "PRESIGN"
    params {
      key        = "output.key"
      expires_in = "3600"
    }
  }

  transform {
    url        = "enriched.url.presigned_url"
    expires_at = "enriched.url.expires_at"
    filename   = "output.filename"
  }
}

# List files in S3 bucket
flow "list_s3_files" {
  from {
    connector = "api"
    operation = "GET /files"
  }

  to {
    connector = "s3"
    operation = "LIST"
    target    = "input.prefix"
  }
}

# Delete from S3
flow "delete_from_s3" {
  from {
    connector = "api"
    operation = "DELETE /files/:id"
  }

  # Get metadata
  to {
    connector = "db"
    target    = "files"
  }

  # Delete from S3
  after {
    to {
      connector = "s3"
      operation = "DELETE"
      target    = "output.key"
    }
  }

  # Delete metadata
  after {
    to {
      connector = "db"
      target    = "files"
      operation = "DELETE"
    }
  }
}

# Copy file within S3
flow "copy_s3_file" {
  from {
    connector = "api"
    operation = "POST /copy/:id"
  }

  # Get source file
  to {
    connector = "db"
    target    = "files"
  }

  transform {
    id           = "uuid()"
    key          = "'copies/' + uuid() + '/' + output.filename"
    filename     = "output.filename"
    source_key   = "output.key"
    size         = "output.size"
    mime_type    = "output.mime_type"
    created_at   = "now()"
  }

  # Copy in S3
  after {
    to {
      connector = "s3"
      operation = "COPY"
      params {
        source = "output.source_key"
        dest   = "output.key"
      }
    }
  }

  # Save new metadata
  after {
    to {
      connector = "db"
      target    = "files"
      operation = "INSERT"
    }
  }
}

# S3 File Operations Flows
#
# NOTE: This is a simplified example. Advanced S3 operations require
# parser support for 'operation' in 'to' blocks and 'after' hooks.

# Download from S3 - get metadata
flow "get_file_metadata" {
  from {
    connector = "api"
    operation = "GET /files/:id"
  }

  to {
    connector = "db"
    target    = "files"
  }
}

# List file metadata from database
flow "list_files" {
  from {
    connector = "api"
    operation = "GET /files"
  }

  to {
    connector = "db"
    target    = "files"
  }
}

# =========================================
# Advanced Features (Documented, Need Parser Support)
# =========================================
# The following patterns require parser enhancements:
#
# 1. S3 WRITE operation:
#    to {
#      connector = "s3"
#      operation = "WRITE"
#      target    = "input.key"
#    }
#
# 2. S3 READ operation via enrich:
#    enrich "file" {
#      connector = "s3"
#      operation = "READ"
#      params { key = "output.key" }
#    }
#
# 3. S3 PRESIGN operation:
#    enrich "url" {
#      connector = "s3"
#      operation = "PRESIGN"
#      params { key = "output.key", expires_in = "3600" }
#    }
#
# 4. S3 LIST operation:
#    to {
#      connector = "s3"
#      operation = "LIST"
#      target    = "input.prefix"
#    }
#
# 5. S3 DELETE operation in after block:
#    after {
#      to {
#        connector = "s3"
#        operation = "DELETE"
#        target    = "output.key"
#      }
#    }
#
# 6. S3 COPY operation:
#    after {
#      to {
#        connector = "s3"
#        operation = "COPY"
#        params { source = "...", dest = "..." }
#      }
#    }
#
# See docs/INTEGRATION-PATTERNS.md for full documentation.

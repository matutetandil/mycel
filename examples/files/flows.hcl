# File Operations Flows
# NOTE: This is a simplified example. Advanced features like
# 'operation' in 'to' blocks and 'after' blocks with nested 'to'
# require parser support.

# List files from database metadata
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

# Get file metadata
flow "get_file" {
  from {
    connector = "api"
    operation = "GET /files/:id"
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
# 1. File upload with transform and storage operation:
#    flow "upload_file" {
#      from {
#        connector = "api"
#        operation = "POST /files"
#      }
#      transform { ... }
#      to {
#        connector = "storage"
#        operation = "WRITE"     # Not supported in to block
#        target    = "input.path"
#      }
#    }
#
# 2. After hooks with nested operations:
#    after {
#      to {
#        connector = "db"
#        target    = "files"
#        operation = "INSERT"
#      }
#    }
#
# 3. File download with enrichment:
#    enrich "content" {
#      connector = "storage"
#      operation = "READ"
#      params {
#        path = "output.path"
#      }
#    }
#
# 4. Directory listing:
#    flow "list_directory" {
#      from { connector = "api", operation = "GET /files/dir/:path" }
#      to {
#        connector = "storage"
#        operation = "LIST"      # Not supported in to block
#        target    = "input.path"
#      }
#    }
#
# See docs/INTEGRATION-PATTERNS.md for full documentation.

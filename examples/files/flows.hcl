# File Operations Flows

# Upload a file
flow "upload_file" {
  from {
    connector = "api"
    operation = "POST /files"
  }

  transform {
    id         = "uuid()"
    filename   = "input.filename"
    path       = "'/uploads/' + uuid() + '/' + input.filename"
    size       = "input.size"
    mime_type  = "input.mime_type"
    created_at = "now()"
  }

  to {
    connector = "storage"
    operation = "WRITE"
    target    = "input.path"
  }

  # Also save metadata to database
  after {
    to {
      connector = "db"
      target    = "files"
      operation = "INSERT"
    }
  }
}

# Download a file
flow "download_file" {
  from {
    connector = "api"
    operation = "GET /files/:id/download"
  }

  # Get file metadata from DB
  to {
    connector = "db"
    target    = "files"
  }

  # Return file content
  enrich "content" {
    connector = "storage"
    operation = "READ"
    params {
      path = "output.path"
    }
  }

  transform {
    filename = "output.filename"
    content  = "enriched.content.data"
    mime_type = "output.mime_type"
  }
}

# List files
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

# Delete a file
flow "delete_file" {
  from {
    connector = "api"
    operation = "DELETE /files/:id"
  }

  # Get file path from DB
  to {
    connector = "db"
    target    = "files"
  }

  # Delete from storage
  after {
    to {
      connector = "storage"
      operation = "DELETE"
      target    = "output.path"
    }
  }

  # Delete metadata from DB
  after {
    to {
      connector = "db"
      target    = "files"
      operation = "DELETE"
    }
  }
}

# List files in a directory
flow "list_directory" {
  from {
    connector = "api"
    operation = "GET /files/dir/:path"
  }

  to {
    connector = "storage"
    operation = "LIST"
    target    = "input.path"
  }
}

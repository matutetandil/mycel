# Files Example Configuration
# This example shows how to work with local files

service {
  name    = "files-example"
  version = "1.0.0"
}

# REST API to expose file operations
connector "api" {
  type = "rest"
  port = 3000
}

# Local file system connector
#
# NOTE: File connector uses current directory as base path.
# The connector will create directories as needed (create_dirs=true by default).
# Format defaults to "json". See internal/connector/file/factory.go for options.
connector "storage" {
  type   = "file"
  driver = "local"

  # NOTE: base_path and permissions block need parser support:
  # base_path = "./data/files"
  # permissions {
  #   file_mode = "0644"
  #   dir_mode  = "0755"
  # }
}

# Database to track file metadata
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/files.db"
}

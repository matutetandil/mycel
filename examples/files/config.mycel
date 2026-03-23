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
connector "storage" {
  type   = "file"
  driver = "local"

  # File connector configuration
  base_path   = "./data/files"
  format      = "json"
  create_dirs = true
  permissions = "0644"

  # Optional: Watch for file changes
  watch          = true
  watch_interval = "5s"
}

# Database to track file metadata
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/files.db"
}

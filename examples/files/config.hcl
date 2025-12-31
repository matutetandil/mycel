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
  type      = "file"
  driver    = "local"
  base_path = "./data/files"

  permissions {
    file_mode = "0644"
    dir_mode  = "0755"
  }
}

# Database to track file metadata
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/files.db"
}

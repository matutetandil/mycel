service {
  name    = "file-processor"
  version = "1.0.0"
}

# SFTP server for receiving files from partners
connector "partner_sftp" {
  type      = "ftp"
  protocol  = "sftp"
  host      = "sftp.partner.com"
  port      = 22
  username  = env("SFTP_USER")
  password  = env("SFTP_PASS")
  base_path = "/incoming"
}

# Local API
connector "api" {
  type = "rest"
  port = 3000
}

# Database
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = "localhost"
  database = "files"
  user     = "admin"
  password = env("DB_PASS")
}

# List files on the SFTP server
flow "list_files" {
  from {
    connector = "api"
    operation = "GET /files"
  }
  to {
    connector = "partner_sftp"
    target    = "/reports"
  }
}

# Download and process a file
flow "download_file" {
  from {
    connector = "api"
    operation = "GET /files/:path"
  }
  to {
    connector = "partner_sftp"
    target    = "input.path"
  }
}

# Upload a processed file back
flow "upload_result" {
  from {
    connector = "api"
    operation = "POST /files/upload"
  }
  transform {
    _content  = "input.content"
    _filename = "input.filename"
  }
  to {
    connector = "partner_sftp"
    target    = "input.filename"
  }
}

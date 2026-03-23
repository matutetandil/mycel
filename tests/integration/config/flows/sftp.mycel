# SFTP flows for integration tests

# List files on SFTP server
flow "sftp_list" {
  from {
    connector = "api"
    operation = "GET /sftp/files"
  }
  to {
    connector = "sftp_server"
    target    = "/"
    operation = "LIST"
  }
}

# Upload a file to SFTP server
flow "sftp_upload" {
  from {
    connector = "api"
    operation = "POST /sftp/upload"
  }
  transform {
    _content = "input.content"
  }
  to {
    connector = "sftp_server"
    target    = "test-upload.json"
    operation = "PUT"
  }
}

# Download a file from SFTP server
flow "sftp_download" {
  from {
    connector = "api"
    operation = "GET /sftp/download"
  }
  to {
    connector = "sftp_server"
    target    = "test-upload.json"
    operation = "GET"
  }
}

# Delete a file from SFTP server
flow "sftp_delete" {
  from {
    connector = "api"
    operation = "DELETE /sftp/files"
  }
  to {
    connector = "sftp_server"
    target    = "test-upload.json"
    operation = "DELETE"
  }
}

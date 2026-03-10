# FTP / SFTP

Read and write files on remote FTP and SFTP servers. Supports directory listing, file download with automatic format detection (JSON, CSV, text), file upload, directory creation, and file deletion. Use it for legacy system integration, partner file exchanges, batch file processing, or any scenario involving remote file transfer.

## Configuration

```hcl
connector "partner_sftp" {
  type      = "ftp"
  protocol  = "sftp"
  host      = "sftp.partner.com"
  port      = 22
  username  = env("SFTP_USER")
  password  = env("SFTP_PASS")
  base_path = "/incoming"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `protocol` | string | `"ftp"` | Protocol: `"ftp"` or `"sftp"` |
| `host` | string | — | Server hostname or IP |
| `port` | int | `21` (FTP) / `22` (SFTP) | Server port |
| `username` | string | — | Authentication username |
| `password` | string | — | Authentication password |
| `base_path` | string | — | Remote base directory for all operations |
| `key_file` | string | — | SSH private key file (SFTP only) |
| `passive` | bool | `true` | Enable FTP passive mode |
| `timeout` | duration | `"30s"` | Connection timeout |
| `tls` | bool | `false` | Enable explicit TLS (FTPS) |

## Operations

### Read Operations

| Operation | Description |
|-----------|-------------|
| `LIST` | List files in a directory. Returns `[{name, size, mod_time, is_dir}]` |
| `GET` (or empty) | Download a file. Format auto-detected from extension (`.json`, `.csv`, `.txt`) |

### Write Operations

| Operation | Description |
|-----------|-------------|
| `PUT` / `UPLOAD` (or empty) | Upload content to a remote path |
| `MKDIR` | Create a remote directory |
| `DELETE` | Remove a remote file |

### Download Format Detection

Files are automatically parsed based on extension:

| Extension | Format | Result |
|-----------|--------|--------|
| `.json` | JSON | Parsed JSON object or array |
| `.csv` | CSV | Array of objects (header row = keys) |
| `.txt`, `.log`, `.md` | Text | `{_path, _name, _content, _size}` |
| Other | Binary | `{_path, _name, _content, _size}` (raw bytes) |

Override with `format` parameter: `params = { format = "csv" }`.

### Upload Payload

For uploads, use `_content` in the transform to set file content:

```hcl
transform {
  _content  = "input.content"
  _filename = "input.filename"
}
```

If `_content` is not present, the entire payload is JSON-serialized and uploaded.

## Example

```hcl
# List files on SFTP server
flow "list_files" {
  from { connector = "api", operation = "GET /files" }
  to   { connector = "partner_sftp", target = "/reports" }
}

# Download a file
flow "download_file" {
  from { connector = "api", operation = "GET /files/:path" }
  to   { connector = "partner_sftp", target = "input.path" }
}

# Upload a file
flow "upload_result" {
  from { connector = "api", operation = "POST /files/upload" }
  transform {
    _content  = "input.content"
    _filename = "input.filename"
  }
  to { connector = "partner_sftp", target = "input.filename" }
}
```

See the [ftp example](../../examples/ftp/) for a complete working setup.

---

> **Full configuration reference:** See [FTP](../reference/configuration.md#ftp) in the Configuration Reference.

# Filesystem

Read and write files on the local filesystem. Use it for file-based integrations, data import/export, or serving static content.

## Configuration

```hcl
connector "files" {
  type      = "file"
  driver    = "local"
  base_path = "/data/files"

  permissions {
    file_mode = "0644"
    dir_mode  = "0755"
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | — | Must be `local` |
| `base_path` | string | — | Root directory for all operations |
| `permissions.file_mode` | string | `"0644"` | Default file permissions |
| `permissions.dir_mode` | string | `"0755"` | Default directory permissions |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `read` | read | Read a file's contents |
| `write` | write | Write contents to a file |
| `list` | read | List files in a directory |
| `delete` | write | Delete a file |

## Example

```hcl
flow "save_report" {
  from { connector = "api", operation = "POST /reports" }
  to   { connector = "files", operation = "write", target = "reports/" }
}

flow "read_config" {
  from { connector = "api", operation = "GET /config/:name" }
  to   { connector = "files", operation = "read" }
}
```

See the [files example](../../examples/files/) for a complete working setup.

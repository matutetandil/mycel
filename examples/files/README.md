# Files Example

This example demonstrates local file system operations.

## Features

- Upload and download files
- List directory contents
- File metadata tracking in SQLite
- Delete files with metadata cleanup

## Files

- `config.hcl` - Connectors configuration
- `flows.hcl` - File operation flows

## Usage

```bash
# Start the service
mycel start --config ./examples/files

# Upload a file
curl -X POST http://localhost:3000/files \
  -H "Content-Type: application/json" \
  -d '{"filename":"test.txt","content":"Hello World","mime_type":"text/plain"}'

# List files
curl http://localhost:3000/files

# Download a file
curl http://localhost:3000/files/{id}/download

# Delete a file
curl -X DELETE http://localhost:3000/files/{id}
```

## Configuration

```hcl
connector "storage" {
  type      = "file"
  driver    = "local"
  base_path = "./data/files"

  permissions {
    file_mode = "0644"
    dir_mode  = "0755"
  }
}
```

## Operations

| Operation | Description |
|-----------|-------------|
| `READ` | Read file content |
| `WRITE` | Write file content |
| `DELETE` | Delete a file |
| `LIST` | List directory contents |

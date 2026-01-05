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

## Verify It Works

### 1. Start the service

```bash
mycel start --config ./examples/files
```

You should see:
```
INFO  Starting service: files-example
INFO  Loaded 3 connectors: api, storage, db
INFO    storage: local filesystem at ./data/files
INFO  REST server listening on :3000
```

### 2. Upload a file

```bash
curl -X POST http://localhost:3000/files \
  -H "Content-Type: application/json" \
  -d '{"filename":"test.txt","content":"Hello World","mime_type":"text/plain"}'
```

Expected response:
```json
{
  "id": "<uuid>",
  "filename": "test.txt",
  "size": 11,
  "mime_type": "text/plain",
  "created_at": "2024-01-15T10:30:00Z"
}
```

### 3. Verify file exists

```bash
ls -la ./data/files/
# Should show: test.txt
```

### 4. List files

```bash
curl http://localhost:3000/files
```

Expected response:
```json
[{"id": "<uuid>", "filename": "test.txt", "size": 11}]
```

### 5. Download file

```bash
curl http://localhost:3000/files/<uuid>/download
```

Expected response:
```
Hello World
```

### Common Issues

**"Permission denied"**

Ensure the `base_path` directory is writable:
```bash
mkdir -p ./data/files
chmod 755 ./data/files
```

**"File not found"**

Check that the file ID is correct and the file was successfully uploaded.

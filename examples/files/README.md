# Files Example

This example demonstrates local file system operations.

## Features

- Upload a file and record it, in one flow with two destinations
- Download the bytes back
- List what has been stored

## Usage

The database file and the storage directory are created where the service is
started from, so run these from this directory.

```bash
cd examples/files

# Create the table that indexes what has been stored
mycel migrate --config .

mycel start --config .
```

### Upload a file

The bytes go to the file connector and what is known about them to the
database — one flow, two destinations.

```bash
curl -X POST http://localhost:3000/files \
  -H "Content-Type: application/json" \
  -d '{"filename":"test.txt","content":"Hello World"}'
```

```json
{"results":{"db":{"affected":1,"id":1},"storage":{"affected":1,"id":null}},"success":true}
```

The file itself is now at `data/files/test.txt`.

### List what has been stored

```bash
curl http://localhost:3000/files
```

```json
[{"filename":"test.txt","size":11,"uploaded_at":"2026-08-19T19:24:33Z"}]
```

### What is known about one file

```bash
curl http://localhost:3000/files/test.txt
```

```json
[{"filename":"test.txt","size":11,"uploaded_at":"2026-08-19T19:24:33Z"}]
```

### Download the bytes

```bash
curl http://localhost:3000/files/test.txt/content
```

```json
[{"content":"Hello World"}]
```

## Configuration

```hcl
connector "storage" {
  type      = "file"
  driver    = "local"
  base_path = "./data/files"

  # Octal, as chmod takes it.
  permissions = "0644"
  create_dirs = true
}
```

`base_path` is what confines the writing: an upload names its own file, and a
name that tries to climb out of the directory is resolved back into it.

## Operations

A flow's destination may name an operation; without one, reading a file and
writing a file are chosen by the flow's direction.

| Operation | Description |
|-----------|-------------|
| `read` | Read file content |
| `write` | Write file content |
| `delete` | Delete a file |
| `list` | List directory contents |
| `exists` | Whether a file is there |
| `stat` | Size and modification time |
| `copy` / `move` | Copy or move a file |

## Files

- `config.mycel` — connectors
- `flows.mycel` — the flows
- `migrations/` — the table that indexes stored files

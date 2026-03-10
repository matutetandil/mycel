# Filesystem

Read and write files on the local filesystem. Supports JSON, CSV, Excel, text, and binary formats with automatic detection by extension.

## Configuration

```hcl
connector "files" {
  type           = "file"
  driver         = "local"
  base_path      = "/data/files"
  format         = "json"
  create_dirs    = true
  permissions    = "0644"
  watch          = true
  watch_interval = "5s"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | — | Must be `local` |
| `base_path` | string | `""` | Root directory; all paths resolve relative to this |
| `format` | string | `"json"` | Default format when extension is unknown |
| `create_dirs` | bool | `true` | Auto-create parent directories on write |
| `permissions` | string | `"0644"` | Default file permissions (octal) |
| `watch` | bool | `false` | Enable file-change polling |
| `watch_interval` | string | — | Polling interval (e.g. `"5s"`, `"1m"`) |

## Formats

Format is auto-detected from the file extension. Override with `params = { format = "csv" }`.

### json

**Extensions:** `.json` — **Read output:** array of objects

```jsonc
// file: users.json
[{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]

// read result → same array
[{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]

// single objects are wrapped in an array automatically
// file: config.json → {"debug": true}
// read result → [{"debug": true}]
```

### csv

**Extensions:** `.csv` — **Read output:** array of objects keyed by header row

```csv
id,name,email
1,Alice,alice@example.com
2,Bob,bob@example.com
```

```jsonc
// read result →
[
  {"id": "1", "name": "Alice", "email": "alice@example.com"},
  {"id": "2", "name": "Bob",   "email": "bob@example.com"}
]
```

All values are returned as strings. The first row is always treated as column headers.

### excel

**Extensions:** `.xlsx`, `.xls` — **Read output:** array of objects keyed by header row (same as CSV)

```jsonc
// Sheet "Products":
//   | sku     | price | stock |
//   | ABC-123 | 29.99 | 100   |
//   | DEF-456 | 49.99 | 50    |

// read result →
[
  {"sku": "ABC-123", "price": "29.99", "stock": "100"},
  {"sku": "DEF-456", "price": "49.99", "stock": "50"}
]
```

- First row = headers, subsequent rows = data
- Empty rows are skipped automatically
- Reads the first sheet by default; specify a sheet with `params = { sheet = "Products" }`
- All values are returned as strings (same as CSV)

### text

**Extensions:** `.txt`, `.log`, `.md` — **Read output:** single object with `content`

```jsonc
// read result →
[{"content": "the entire file contents as a string"}]
```

### lines

**No auto-detect** — must set `params = { format = "lines" }`. Returns one object per line:

```jsonc
// read result →
[
  {"line": 1, "content": "first line"},
  {"line": 2, "content": "second line"}
]
```

### binary

**No auto-detect** — must set `params = { format = "binary" }`. Returns raw bytes:

```jsonc
// read result →
[{"data": "<raw bytes>", "size": 4096}]
```

## Operations

| Operation | Description | Parameters |
|-----------|-------------|------------|
| `read` | Read and parse a file | `path`, `format`, `sheet` (excel only) |
| `write` | Write data to a file | `path`, `content`, `format`, `append` |
| `list` | List directory contents | `path` |
| `delete` | Delete a file or directory | `path` |
| `exists` | Check if a file exists | `path` |
| `stat` | Get file metadata | `path` |
| `copy` | Copy a file | `source`, `destination` |
| `move` | Move/rename a file | `source`, `destination` |

### Read

Reads a file and parses it according to the detected or specified format. If the target is a directory, returns a directory listing instead.

```hcl
# Auto-detect format from extension
flow "load_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "files"
    operation = "read"
    target    = "users.json"
  }
}

# Override format explicitly
flow "load_data" {
  from {
    connector = "api"
    operation = "GET /data"
  }
  to   {
    connector = "files"
    operation = "read"
    target    = "data.txt"
    params    = { format = "lines" }
  }
}

# Read a specific Excel sheet
flow "import_products" {
  from {
    connector = "api"
    operation = "POST /import"
  }
  to   {
    connector = "files"
    operation = "read"
    target    = "catalog.xlsx"
    params    = { sheet = "Products" }
  }
}
```

### Write

Writes data to a file. Creates parent directories automatically when `create_dirs = true`.

```hcl
# Write JSON (auto-detected from extension)
flow "save_report" {
  from {
    connector = "api"
    operation = "POST /reports"
  }
  to {
    connector = "files"
    operation = "write"
    target    = "reports/latest.json"
  }
}

# Write CSV
flow "export_users" {
  from {
    connector = "db"
    operation = "users"
  }
  to {
    connector = "files"
    operation = "write"
    target    = "export/users.csv"
  }
}

# Write Excel
flow "export_report" {
  from {
    connector = "db"
    operation = "report_data"
  }
  to {
    connector = "files"
    operation = "write"
    target    = "reports/monthly.xlsx"
  }
}

# Append to a log file
flow "audit_log" {
  from {
    connector = "api"
    operation = "POST /actions"
  }
  to   {
    connector = "files"
    operation = "write"
    target    = "logs/audit.txt"
    params    = { format = "text", append = true }
  }
}
```

**Write returns:**
```jsonc
{"path": "/data/files/reports/latest.json", "written": 1024}
```

### List

Returns metadata for each entry in a directory:

```jsonc
// list result →
[
  {"name": "users.json", "path": "/data/files/users.json", "is_dir": false, "size": 1024, "mod_time": "...", "mode": "-rw-r--r--"},
  {"name": "reports",    "path": "/data/files/reports",    "is_dir": true,  "size": 96,   "mod_time": "...", "mode": "drwxr-xr-x"}
]
```

### Exists / Stat

```hcl
# Check if a file exists
flow "check_file" {
  from {
    connector = "api"
    operation = "GET /files/:name/exists"
  }
  to {
    connector = "files"
    operation = "exists"
  }
}
```

```jsonc
// exists result →  {"exists": true, "path": "/data/files/report.json"}

// stat result →
{
  "name": "report.json",
  "path": "/data/files/report.json",
  "size": 2048,
  "is_dir": false,
  "mod_time": "2026-03-01T10:00:00Z",
  "mode": "-rw-r--r--"
}
```

### Copy / Move

```hcl
# Copy a file
flow "backup_config" {
  from {
    connector = "scheduler"
    operation = "daily"
  }
  to   {
    connector = "files"
    operation = "copy"
    params    = { source = "config.json", destination = "backups/config.json" }
  }
}
```

```jsonc
// copy result →  {"copied": true, "source": "...", "dest": "...", "bytes": 1024}
// move result →  {"moved": true, "source": "...", "dest": "..."}
```

## Examples

### Import an Excel file via REST

```hcl
connector "api" {
  type   = "rest"
  driver = "server"
  port   = 8080
}

connector "storage" {
  type        = "file"
  driver      = "local"
  base_path   = "./data"
  create_dirs = true
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

# Upload an Excel file, read it, and insert rows into the database
flow "import_users" {
  from {
    connector = "api"
    operation = "POST /import/users"
  }

  step "read_file" {
    connector = "storage"
    operation = "read"
    target    = "uploads/users.xlsx"
    params    = { sheet = "Users" }
  }

  to {
    connector = "db"
    operation = "INSERT"
    target    = "users"
  }
}
```

### Export database to CSV

```hcl
flow "export_orders" {
  from {
    connector = "api"
    operation = "GET /export/orders"
  }

  step "query" {
    connector = "db"
    operation = "SELECT * FROM orders WHERE status = 'completed'"
  }

  to {
    connector = "storage"
    operation = "write"
    target    = "exports/orders.csv"
  }
}
```

## Watch Mode

When `watch = true`, the file connector polls the `base_path` directory for new and modified files. When a matching file is detected, the associated flow handler is triggered automatically — similar to how MQ connectors trigger flows on new messages.

### Configuration

```hcl
connector "inbox" {
  type           = "file"
  driver         = "local"
  base_path      = "/data/inbox"
  watch          = true
  watch_interval = "5s"    # default: 5s
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `watch` | bool | `false` | Enable directory polling for file changes |
| `watch_interval` | string | `"5s"` | How often to scan for changes (e.g. `"5s"`, `"1m"`, `"500ms"`) |

### Flow Syntax

The `operation` in the `from` block is a glob pattern that selects which files trigger the flow:

```hcl
# Trigger on any new/modified CSV file
flow "process_csv" {
  from {
    connector = "inbox"
    operation = "*.csv"
  }
  to {
    connector = "db"
    operation = "INSERT"
    target    = "imports"
  }
}

# Trigger on CSV files in a specific subdirectory
flow "process_reports" {
  from {
    connector = "inbox"
    operation = "reports/*.csv"
  }
  to {
    connector = "db"
    operation = "INSERT"
    target    = "report_data"
  }
}
```

### Handler Input

When a file event fires, the handler receives file metadata (prefixed with `_`) merged with parsed file content:

```jsonc
{
  "_path":     "invoices/INV-001.csv",     // relative path from base_path
  "_name":     "INV-001.csv",              // filename only
  "_size":     1234,                       // file size in bytes
  "_mod_time": "2026-03-05T12:00:00Z",    // last modification time (RFC 3339)
  "_event":    "created",                  // "created" or "modified"

  // Parsed file content (auto-detected from extension):
  // Single-row files → fields merged into the input
  // Multi-row files → available as "rows" array
  "rows": [
    {"id": "1", "product": "Widget", "amount": "100"},
    {"id": "2", "product": "Gadget", "amount": "200"}
  ]
}
```

### How It Works

1. On startup, the watcher snapshots all existing files (no events fired)
2. Every `watch_interval`, it scans the directory tree recursively
3. New files trigger a `"created"` event; files with changed size or modification time trigger `"modified"`
4. Unchanged files are ignored
5. The file is read and parsed using the same format auto-detection as the `read` operation

### Example: Process Incoming CSV Files

```hcl
connector "inbox" {
  type           = "file"
  driver         = "local"
  base_path      = "/data/inbox"
  watch          = true
  watch_interval = "5s"
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

flow "import_csv" {
  from {
    connector = "inbox"
    operation = "*.csv"
  }

  transform {
    output.file   = input._path
    output.name   = input._name
    output.data   = input.rows
  }

  to {
    connector = "db"
    operation = "INSERT"
    target    = "imports"
  }
}
```

See the [files example](../../examples/files/) for a complete working setup.

---

> **Full configuration reference:** See [File System](../reference/configuration.md#file-system) in the Configuration Reference.

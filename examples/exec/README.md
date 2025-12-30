# Exec Connector Example

This example demonstrates how to use the `exec` connector to execute external commands locally or via SSH.

## Overview

The exec connector allows you to:
- Execute local shell commands
- Execute commands on remote servers via SSH
- Use command output as data in your flows
- Process input data through external programs
- Integrate with existing scripts and CLI tools

## Drivers

| Driver | Description |
|--------|-------------|
| `local` | Execute commands on the local machine |
| `ssh` | Execute commands on remote servers via SSH |

## Configuration

### Local Execution

```hcl
connector "my_command" {
  type   = "exec"
  driver = "local"

  command       = "echo"
  args          = ["hello", "world"]
  timeout       = "10s"
  output_format = "text"  # text, json, lines
}
```

### Shell Execution

For commands with pipes, environment variables, or shell features:

```hcl
connector "shell_command" {
  type   = "exec"
  driver = "local"

  command       = "df -h / | tail -1"
  shell         = "bash -c"
  timeout       = "5s"
  output_format = "text"
}
```

### SSH Remote Execution

```hcl
connector "remote_server" {
  type   = "exec"
  driver = "ssh"

  command = "uptime"
  timeout = "30s"

  ssh {
    host       = "server.example.com"
    port       = 22
    user       = "admin"
    key_file   = "/path/to/private_key"
    # password = "..." # Not recommended, use key_file instead
  }
}
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `command` | string | required | The command to execute |
| `args` | list | `[]` | Arguments to pass to the command |
| `workdir` | string | `""` | Working directory for execution |
| `timeout` | duration | `30s` | Maximum execution time |
| `shell` | string | `""` | Shell wrapper (e.g., `bash -c`) |
| `input_format` | string | `json` | How to pass input: `args`, `stdin`, `json` |
| `output_format` | string | `json` | How to parse output: `text`, `json`, `lines` |
| `env` | map | `{}` | Environment variables |

### SSH Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | required | Remote host |
| `port` | int | `22` | SSH port |
| `user` | string | required | SSH username |
| `key_file` | string | `""` | Path to private key file |
| `password` | string | `""` | SSH password (not recommended) |
| `known_hosts` | string | `""` | Path to known_hosts file |

## Output Formats

### text
Returns the raw output as a single string:
```json
{"output": "hello world\n"}
```

### json
Parses the output as JSON:
```json
{"name": "test", "value": 42}
```

### lines
Splits output by newlines:
```json
[
  {"line": 1, "value": "first line"},
  {"line": 2, "value": "second line"}
]
```

## Input Formats

### args
Passes input as command-line arguments:
```hcl
input_format = "args"
# Input: {"name": "test", "value": 42}
# Becomes: command --name=test --value=42
```

### stdin / json
Sends input as JSON via stdin:
```hcl
input_format = "json"
# Input is JSON-encoded and sent to stdin
```

## Use Cases

### 1. Execute Local Scripts
```hcl
connector "backup_script" {
  type    = "exec"
  driver  = "local"
  command = "/scripts/backup.sh"
  args    = ["--full"]
  timeout = "5m"
}
```

### 2. Remote Server Monitoring
```hcl
connector "server_status" {
  type    = "exec"
  driver  = "ssh"
  command = "systemctl status nginx"

  ssh {
    host     = "web-server.example.com"
    user     = "monitor"
    key_file = "/etc/mycel/keys/monitor.pem"
  }
}
```

### 3. Data Enrichment via External API
```hcl
connector "external_api" {
  type          = "exec"
  driver        = "local"
  command       = "curl"
  args          = ["-s", "https://api.example.com/data"]
  output_format = "json"
}

flow "get_enriched" {
  from { ... }

  enrich "api_data" {
    connector = "external_api"
    operation = ""
  }

  transform {
    id   = "input.id"
    data = "enriched.api_data"
  }

  to { ... }
}
```

### 4. Process Data Through External Program
```hcl
connector "data_transformer" {
  type          = "exec"
  driver        = "local"
  command       = "jq"
  args          = [".items | map(.name)"]
  input_format  = "stdin"
  output_format = "json"
}
```

## Running This Example

```bash
# Start the service
mycel start --config ./examples/exec

# Test endpoints
curl http://localhost:3000/system/info
curl http://localhost:3000/system/disk
curl -X POST http://localhost:3000/echo -d '{"message":"hello"}'
```

## See Also

- [TCP Example](../tcp/README.md) - TCP connector usage
- [Enrich Example](../enrich/README.md) - Data enrichment from external services
- [Message Queue Example](../mq/README.md) - RabbitMQ/Kafka integration

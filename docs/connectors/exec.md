# Exec

Execute shell commands locally or over SSH. Use it for running scripts, calling CLI tools, triggering builds, or any system-level operation.

## Local Configuration

```hcl
connector "script" {
  type   = "exec"
  driver = "local"

  command       = "/usr/bin/python3"
  args          = ["script.py"]
  timeout       = "30s"
  working_dir   = "/app/scripts"
  input_format  = "json"     # "args", "stdin", "json"
  output_format = "json"     # "text", "json", "lines"

  env {
    CUSTOM_VAR = "value"
  }
}
```

## SSH Configuration

```hcl
connector "remote" {
  type   = "exec"
  driver = "ssh"

  command = "uptime"

  ssh {
    host     = "server.example.com"
    port     = 22
    user     = "admin"
    key_file = "/path/to/key"
  }
}
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | — | `local` or `ssh` |
| `command` | string | — | Command to execute |
| `args` | list | — | Command arguments |
| `timeout` | duration | `"30s"` | Execution timeout |
| `working_dir` | string | — | Working directory (local) |
| `input_format` | string | `"json"` | How to pass input: `args`, `stdin`, `json` |
| `output_format` | string | `"json"` | How to parse output: `text`, `json`, `lines` |
| `env` | map | — | Environment variables |
| `ssh.host` | string | — | Remote host |
| `ssh.port` | int | `22` | SSH port |
| `ssh.user` | string | — | SSH username |
| `ssh.key_file` | string | — | Path to SSH private key |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `execute` | target | Run the command with flow data as input |

## Example

```hcl
flow "run_report" {
  from {
    connector = "api"
    operation = "POST /reports/generate"
  }
  to {
    connector = "script"
    operation = "execute"
  }
}
```

See the [exec example](../../examples/exec/) for a complete working setup.

---

> **Full configuration reference:** See [Exec](../reference/configuration.md#exec) in the Configuration Reference.

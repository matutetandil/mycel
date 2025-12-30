# API connector for exposing HTTP endpoints
connector "api" {
  type = "rest"
  port = 3000
}

# Local command execution - runs commands locally
connector "local_exec" {
  type   = "exec"
  driver = "local"

  command       = "echo"
  output_format = "text"
  timeout       = "10s"
}

# Execute a JSON-outputting command
connector "system_info" {
  type   = "exec"
  driver = "local"

  command       = "uname"
  args          = ["-a"]
  output_format = "text"
  timeout       = "5s"
}

# Execute a shell command with pipes and environment variables
connector "disk_usage" {
  type   = "exec"
  driver = "local"

  command       = "df -h / | tail -1 | awk '{print $5}'"
  shell         = "bash -c"
  output_format = "text"
  timeout       = "5s"
}

# Example: SSH remote execution (requires SSH key setup)
# connector "remote_server" {
#   type   = "exec"
#   driver = "ssh"
#
#   command = "uptime"
#   timeout = "30s"
#
#   ssh {
#     host     = "server.example.com"
#     port     = 22
#     user     = "admin"
#     key_file = "/path/to/private_key"
#   }
# }

# Example: Execute a script that outputs JSON
connector "json_script" {
  type   = "exec"
  driver = "local"

  command       = "echo"
  args          = ["{\"status\":\"ok\",\"timestamp\":\"$(date +%s)\"}"]
  shell         = "bash -c"
  output_format = "json"
  timeout       = "5s"
}

# Example: Process input data via stdin
connector "data_processor" {
  type   = "exec"
  driver = "local"

  command      = "cat"
  input_format = "json"
  timeout      = "10s"
}

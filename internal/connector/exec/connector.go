// Package exec provides a connector for executing external commands.
// It supports local shell execution and remote SSH execution.
package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Connector executes external commands locally or via SSH.
type Connector struct {
	config *Config
	name   string
}

// Config holds the exec connector configuration.
type Config struct {
	// Driver is the execution driver: "local" or "ssh"
	Driver string

	// Command is the command/executable to run
	Command string

	// Args are the default arguments to pass to the command
	Args []string

	// WorkDir is the working directory for command execution
	WorkDir string

	// Timeout is the maximum execution time
	Timeout time.Duration

	// Env are environment variables to set for the command
	Env map[string]string

	// SSH configuration (only used when Driver is "ssh")
	SSH *SSHConfig

	// Shell wraps the command in a shell (e.g., "bash -c")
	Shell string

	// InputFormat is how to pass input data: "args", "stdin", "json"
	InputFormat string

	// OutputFormat is how to parse output: "text", "json", "lines"
	OutputFormat string
}

// SSHConfig holds SSH connection configuration.
type SSHConfig struct {
	Host       string
	Port       int
	User       string
	KeyFile    string
	Password   string // Not recommended, use KeyFile instead
	KnownHosts string
}

// New creates a new exec connector.
func New(name string, config *Config) *Connector {
	// Set defaults
	if config.Driver == "" {
		config.Driver = "local"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.InputFormat == "" {
		config.InputFormat = "json"
	}
	if config.OutputFormat == "" {
		config.OutputFormat = "json"
	}
	if config.SSH != nil && config.SSH.Port == 0 {
		config.SSH.Port = 22
	}

	return &Connector{
		config: config,
		name:   name,
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "exec"
}

// Connect validates the configuration.
func (c *Connector) Connect(ctx context.Context) error {
	if c.config.Command == "" {
		return fmt.Errorf("exec connector requires a command")
	}

	if c.config.Driver == "ssh" && c.config.SSH == nil {
		return fmt.Errorf("exec connector with ssh driver requires ssh configuration")
	}

	return nil
}

// Close is a no-op for exec connector.
func (c *Connector) Close(ctx context.Context) error {
	return nil
}

// Health checks if the command is available.
func (c *Connector) Health(ctx context.Context) error {
	if c.config.Driver == "local" {
		// Check if command exists
		_, err := exec.LookPath(c.config.Command)
		if err != nil {
			return fmt.Errorf("command not found: %s", c.config.Command)
		}
	}
	// For SSH, we'd need to attempt a connection
	return nil
}

// Read executes a command and returns the output as data rows.
// This is used when the exec connector is a source.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// Build command with query filters as arguments or input
	output, err := c.execute(ctx, query.Target, query.Filters)
	if err != nil {
		return nil, err
	}

	// Parse output based on format
	rows, err := c.parseOutput(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command output: %w", err)
	}

	return &connector.Result{
		Rows: rows,
	}, nil
}

// Write executes a command with the provided data.
// This is used when the exec connector is a target.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Execute command with payload as input
	_, err := c.execute(ctx, data.Target, data.Payload)
	if err != nil {
		return nil, err
	}

	return &connector.Result{
		Affected: 1,
	}, nil
}

// Call executes a command with parameters and returns the result.
// This is used for enrichment.
func (c *Connector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	output, err := c.execute(ctx, operation, params)
	if err != nil {
		return nil, err
	}

	// Parse output
	rows, err := c.parseOutput(output)
	if err != nil {
		return nil, err
	}

	// Return single result if only one row
	if len(rows) == 1 {
		return rows[0], nil
	}

	return rows, nil
}

// execute runs the command with the given operation and input.
func (c *Connector) execute(ctx context.Context, operation string, input interface{}) ([]byte, error) {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Build command arguments
	args := c.buildArgs(operation, input)

	var cmd *exec.Cmd
	var stdin bytes.Buffer

	if c.config.Driver == "ssh" {
		// Build SSH command
		cmd = c.buildSSHCommand(ctx, args)
	} else {
		// Local execution
		if c.config.Shell != "" {
			// Wrap in shell
			shellParts := strings.Fields(c.config.Shell)
			fullCmd := c.config.Command + " " + strings.Join(args, " ")
			shellArgs := append(shellParts[1:], fullCmd)
			cmd = exec.CommandContext(ctx, shellParts[0], shellArgs...)
		} else {
			cmd = exec.CommandContext(ctx, c.config.Command, args...)
		}
	}

	// Set working directory
	if c.config.WorkDir != "" {
		cmd.Dir = c.config.WorkDir
	}

	// Set environment variables
	if len(c.config.Env) > 0 {
		cmd.Env = make([]string, 0, len(c.config.Env))
		for k, v := range c.config.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Handle stdin input
	if c.config.InputFormat == "stdin" || c.config.InputFormat == "json" {
		if input != nil {
			jsonData, err := json.Marshal(input)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal input: %w", err)
			}
			stdin.Write(jsonData)
			cmd.Stdin = &stdin
		}
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out after %s", c.config.Timeout)
		}
		return nil, fmt.Errorf("command failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// buildArgs builds command arguments from operation and input.
func (c *Connector) buildArgs(operation string, input interface{}) []string {
	args := make([]string, 0, len(c.config.Args)+2)

	// Add default args
	args = append(args, c.config.Args...)

	// Add operation as argument if provided
	if operation != "" {
		args = append(args, operation)
	}

	// Add input as arguments if format is "args"
	if c.config.InputFormat == "args" && input != nil {
		switch v := input.(type) {
		case map[string]interface{}:
			for key, val := range v {
				args = append(args, fmt.Sprintf("--%s=%v", key, val))
			}
		case []interface{}:
			for _, val := range v {
				args = append(args, fmt.Sprintf("%v", val))
			}
		}
	}

	return args
}

// buildSSHCommand creates an SSH command.
func (c *Connector) buildSSHCommand(ctx context.Context, args []string) *exec.Cmd {
	sshArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
	}

	if c.config.SSH.KeyFile != "" {
		sshArgs = append(sshArgs, "-i", c.config.SSH.KeyFile)
	}

	sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", c.config.SSH.Port))

	// Build user@host
	target := c.config.SSH.Host
	if c.config.SSH.User != "" {
		target = c.config.SSH.User + "@" + c.config.SSH.Host
	}
	sshArgs = append(sshArgs, target)

	// Build remote command
	remoteCmd := c.config.Command
	if len(args) > 0 {
		remoteCmd += " " + strings.Join(args, " ")
	}
	sshArgs = append(sshArgs, remoteCmd)

	return exec.CommandContext(ctx, "ssh", sshArgs...)
}

// parseOutput parses command output based on the configured format.
func (c *Connector) parseOutput(output []byte) ([]map[string]interface{}, error) {
	if len(output) == 0 {
		return []map[string]interface{}{}, nil
	}

	switch c.config.OutputFormat {
	case "json":
		return c.parseJSONOutput(output)
	case "lines":
		return c.parseLinesOutput(output)
	case "text":
		return []map[string]interface{}{
			{"output": string(output)},
		}, nil
	default:
		return c.parseJSONOutput(output)
	}
}

// parseJSONOutput parses JSON output.
func (c *Connector) parseJSONOutput(output []byte) ([]map[string]interface{}, error) {
	// Trim whitespace
	output = bytes.TrimSpace(output)

	// Try parsing as array first
	var arrayResult []map[string]interface{}
	if err := json.Unmarshal(output, &arrayResult); err == nil {
		return arrayResult, nil
	}

	// Try parsing as single object
	var objectResult map[string]interface{}
	if err := json.Unmarshal(output, &objectResult); err == nil {
		return []map[string]interface{}{objectResult}, nil
	}

	// Try parsing as any JSON value
	var anyResult interface{}
	if err := json.Unmarshal(output, &anyResult); err == nil {
		return []map[string]interface{}{{"value": anyResult}}, nil
	}

	// If not valid JSON, return as text
	return []map[string]interface{}{
		{"output": string(output)},
	}, nil
}

// parseLinesOutput parses output as lines.
func (c *Connector) parseLinesOutput(output []byte) ([]map[string]interface{}, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	rows := make([]map[string]interface{}, 0, len(lines))

	for i, line := range lines {
		if line != "" {
			rows = append(rows, map[string]interface{}{
				"line":  i + 1,
				"value": line,
			})
		}
	}

	return rows, nil
}

// Ensure Connector implements the required interfaces.
var (
	_ connector.Connector  = (*Connector)(nil)
	_ connector.Reader     = (*Connector)(nil)
	_ connector.Writer     = (*Connector)(nil)
)

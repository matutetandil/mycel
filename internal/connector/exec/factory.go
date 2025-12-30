package exec

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Factory creates exec connectors.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new exec connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{
		logger: logger,
	}
}

// Supports returns true if this factory can create the specified connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "exec"
}

// Create creates a new exec connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := &Config{
		Driver: cfg.Driver,
		Args:   []string{},
		Env:    make(map[string]string),
	}

	// Parse command
	if cmd, ok := cfg.Properties["command"].(string); ok {
		config.Command = cmd
	}

	// Parse args
	if args, ok := cfg.Properties["args"].([]interface{}); ok {
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				config.Args = append(config.Args, s)
			}
		}
	}

	// Parse workdir
	if workdir, ok := cfg.Properties["workdir"].(string); ok {
		config.WorkDir = workdir
	}

	// Parse timeout
	if timeout, ok := cfg.Properties["timeout"].(string); ok {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
		config.Timeout = d
	}

	// Parse shell
	if shell, ok := cfg.Properties["shell"].(string); ok {
		config.Shell = shell
	}

	// Parse input_format
	if format, ok := cfg.Properties["input_format"].(string); ok {
		config.InputFormat = format
	}

	// Parse output_format
	if format, ok := cfg.Properties["output_format"].(string); ok {
		config.OutputFormat = format
	}

	// Parse env block
	if env, ok := cfg.Properties["env"].(map[string]interface{}); ok {
		for k, v := range env {
			if s, ok := v.(string); ok {
				config.Env[k] = s
			}
		}
	}

	// Parse SSH configuration
	if ssh, ok := cfg.Properties["ssh"].(map[string]interface{}); ok {
		config.SSH = &SSHConfig{}

		if host, ok := ssh["host"].(string); ok {
			config.SSH.Host = host
		}
		if port, ok := ssh["port"].(int); ok {
			config.SSH.Port = port
		}
		if user, ok := ssh["user"].(string); ok {
			config.SSH.User = user
		}
		if keyFile, ok := ssh["key_file"].(string); ok {
			config.SSH.KeyFile = keyFile
		}
		if password, ok := ssh["password"].(string); ok {
			config.SSH.Password = password
		}
		if knownHosts, ok := ssh["known_hosts"].(string); ok {
			config.SSH.KnownHosts = knownHosts
		}

		// Set driver to ssh if ssh config is present
		if config.Driver == "" {
			config.Driver = "ssh"
		}
	}

	// Set default driver
	if config.Driver == "" {
		config.Driver = "local"
	}

	conn := New(cfg.Name, config)

	if f.logger != nil {
		f.logger.Debug("created exec connector",
			"name", cfg.Name,
			"driver", config.Driver,
			"command", config.Command,
		)
	}

	return conn, nil
}

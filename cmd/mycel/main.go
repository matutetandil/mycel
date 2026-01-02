package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/logging"
	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/runtime"
)

// Environment variable names
const (
	EnvEnvironment = "MYCEL_ENV"
)

var (
	// Version information (set at build time)
	version = "0.1.0"
	commit  = "dev"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "mycel",
	Short: "Mycel - Declarative Microservice Framework",
	Long: `Mycel is an open-source framework for creating declarative microservices
through HCL configuration, without writing code.

It works as a single runtime (similar to nginx or Apache) that interprets
configuration files and exposes services.

Philosophy: Configuration, not code. You define WHAT you want, Mycel handles HOW.`,
	Version: fmt.Sprintf("%s (commit: %s)", version, commit),
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Mycel runtime",
	Long: `Start the Mycel runtime and begin serving configured flows.

By default, hot reload is enabled. When you modify any .hcl file in the
configuration directory, Mycel will automatically reload the configuration
without restarting (like nginx).

You can also trigger a manual reload by sending SIGHUP:
  kill -SIGHUP <pid>

To disable hot reload, use --hot-reload=false`,
	RunE: runStart,
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration files",
	Long:  `Validate all HCL configuration files without starting the runtime.`,
	RunE:  runValidate,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check connector connectivity",
	Long:  `Check connectivity to all configured connectors.`,
	RunE:  runCheck,
}

// Flags
var (
	configDir   string
	environment string
	logLevel    string
	logFormat   string
	verbose     bool // deprecated, kept for backward compatibility
	hotReload   bool
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configDir, "config", "c", ".", "Configuration directory")
	rootCmd.PersistentFlags().StringVarP(&environment, "env", "e", "", "Environment (dev, staging, prod). Env: MYCEL_ENV")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level: debug, info, warn, error. Env: MYCEL_LOG_LEVEL")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "Log format: text, json. Env: MYCEL_LOG_FORMAT")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging (deprecated, use --log-level=debug)")

	// Start command flags
	startCmd.Flags().BoolVar(&hotReload, "hot-reload", true, "Enable hot reload (auto-reload on config changes)")

	// Add commands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(checkCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// Setup logger with priority: flag > env var > default
	logger := createLogger()

	// Resolve environment with priority: flag > env var > default
	env := resolveEnvironment()

	// Create runtime
	rt, err := runtime.New(runtime.Options{
		ConfigDir:   configDir,
		Environment: env,
		Logger:      logger,
		HotReload:   hotReload,
	})
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	// Start runtime (blocks until shutdown)
	ctx := context.Background()
	return rt.Start(ctx)
}

// createLogger creates a logger based on flags and environment variables.
// Priority: flag > env var > default
func createLogger() *slog.Logger {
	cfg := logging.DefaultConfig()

	// Check env vars first (will be overridden by flags if set)
	envCfg := logging.ConfigFromEnv()
	cfg.Level = envCfg.Level
	cfg.Format = envCfg.Format

	// Flags override env vars
	if logLevel != "" {
		cfg.Level = logLevel
	} else if verbose {
		// Backward compatibility: --verbose sets debug level
		cfg.Level = "debug"
	}

	if logFormat != "" {
		cfg.Format = logFormat
	}

	return logging.NewLogger(cfg)
}

// resolveEnvironment resolves the environment with priority: flag > env var > default
func resolveEnvironment() string {
	// Flag takes precedence
	if environment != "" {
		return environment
	}

	// Then env var
	if env := os.Getenv(EnvEnvironment); env != "" {
		return env
	}

	// Default
	return "development"
}

func runValidate(cmd *cobra.Command, args []string) error {
	fmt.Printf("Validating configuration...\n")
	fmt.Printf("  Config dir: %s\n", configDir)

	// Parse configuration
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Report success
	fmt.Printf("\n✓ Configuration is valid!\n\n")
	fmt.Printf("  Connectors: %d\n", len(config.Connectors))
	for _, c := range config.Connectors {
		fmt.Printf("    - %s (%s)\n", c.Name, c.Type)
	}

	fmt.Printf("  Flows: %d\n", len(config.Flows))
	for _, f := range config.Flows {
		fmt.Printf("    - %s: %s → %s\n", f.Name, f.From.Operation, f.To.Target)
	}

	fmt.Printf("  Types: %d\n", len(config.Types))
	for _, t := range config.Types {
		fmt.Printf("    - %s (%d fields)\n", t.Name, len(t.Fields))
	}

	return nil
}

func runCheck(cmd *cobra.Command, args []string) error {
	fmt.Printf("Checking connector connectivity...\n")
	fmt.Printf("  Config dir: %s\n", configDir)

	// Setup logger
	logger := createLogger()

	// Resolve environment
	env := resolveEnvironment()

	// Create runtime (which initializes connectors)
	rt, err := runtime.New(runtime.Options{
		ConfigDir:   configDir,
		Environment: env,
		Logger:      logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	// Try to start (this will attempt connections)
	// For now, just validate that we can parse and create the runtime
	fmt.Printf("\n✓ All connectors configured correctly!\n")

	// Clean shutdown
	_ = rt.Shutdown()

	return nil
}

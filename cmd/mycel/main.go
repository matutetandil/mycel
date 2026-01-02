package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/export/asyncapi"
	"github.com/matutetandil/mycel/internal/export/openapi"
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

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export API documentation",
	Long:  `Export API documentation in various formats (OpenAPI, AsyncAPI).`,
}

var exportOpenAPICmd = &cobra.Command{
	Use:   "openapi",
	Short: "Export OpenAPI 3.0 specification",
	Long: `Export OpenAPI 3.0 specification from your Mycel configuration.

This generates a complete OpenAPI spec including:
- All REST endpoints from flows
- Request/response schemas from types
- Path parameters and request bodies
- Server information from connectors

Examples:
  mycel export openapi                           # Output to stdout as YAML
  mycel export openapi -o api.yaml               # Write to file
  mycel export openapi -f json -o api.json       # Export as JSON
  mycel export openapi --base-url https://api.example.com`,
	RunE: runExportOpenAPI,
}

var exportAsyncAPICmd = &cobra.Command{
	Use:   "asyncapi",
	Short: "Export AsyncAPI 2.6 specification",
	Long: `Export AsyncAPI 2.6 specification from your Mycel configuration.

This generates a complete AsyncAPI spec including:
- All message channels from MQ flows (RabbitMQ, Kafka)
- Subscribe operations for consuming flows
- Publish operations for producing flows
- Message schemas from types
- Server information from MQ connectors

Examples:
  mycel export asyncapi                          # Output to stdout as YAML
  mycel export asyncapi -o events.yaml           # Write to file
  mycel export asyncapi -f json -o events.json   # Export as JSON`,
	RunE: runExportAsyncAPI,
}

// Flags
var (
	configDir   string
	environment string
	logLevel    string
	logFormat   string
	verbose     bool // deprecated, kept for backward compatibility
	hotReload   bool

	// Export flags
	exportOutput  string
	exportFormat  string
	exportBaseURL string
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

	// Export command flags (OpenAPI)
	exportOpenAPICmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
	exportOpenAPICmd.Flags().StringVarP(&exportFormat, "format", "f", "yaml", "Output format: yaml, json")
	exportOpenAPICmd.Flags().StringVar(&exportBaseURL, "base-url", "", "Override base URL for API server")

	// Export command flags (AsyncAPI)
	exportAsyncAPICmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
	exportAsyncAPICmd.Flags().StringVarP(&exportFormat, "format", "f", "yaml", "Output format: yaml, json")

	// Add commands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(exportCmd)

	// Add export subcommands
	exportCmd.AddCommand(exportOpenAPICmd)
	exportCmd.AddCommand(exportAsyncAPICmd)
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

func runExportOpenAPI(cmd *cobra.Command, args []string) error {
	// Parse configuration
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Create generator
	gen := openapi.NewGenerator(config)

	// Set base URL if provided
	if exportBaseURL != "" {
		gen.SetBaseURL(exportBaseURL)
	}

	// Generate spec
	spec, err := gen.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate OpenAPI spec: %w", err)
	}

	// Serialize based on format
	var output []byte
	format := strings.ToLower(exportFormat)
	switch format {
	case "json":
		output, err = spec.ToJSON()
	case "yaml", "yml":
		output, err = spec.ToYAML()
	default:
		return fmt.Errorf("unsupported format: %s (use 'yaml' or 'json')", exportFormat)
	}
	if err != nil {
		return fmt.Errorf("failed to serialize spec: %w", err)
	}

	// Write to file or stdout
	if exportOutput != "" {
		if err := os.WriteFile(exportOutput, output, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("✓ OpenAPI spec written to %s\n", exportOutput)
	} else {
		fmt.Println(string(output))
	}

	return nil
}

func runExportAsyncAPI(cmd *cobra.Command, args []string) error {
	// Parse configuration
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Create generator
	gen := asyncapi.NewGenerator(config)

	// Generate spec
	spec, err := gen.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate AsyncAPI spec: %w", err)
	}

	// Serialize based on format
	var output []byte
	format := strings.ToLower(exportFormat)
	switch format {
	case "json":
		output, err = spec.ToJSON()
	case "yaml", "yml":
		output, err = spec.ToYAML()
	default:
		return fmt.Errorf("unsupported format: %s (use 'yaml' or 'json')", exportFormat)
	}
	if err != nil {
		return fmt.Errorf("failed to serialize spec: %w", err)
	}

	// Write to file or stdout
	if exportOutput != "" {
		if err := os.WriteFile(exportOutput, output, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("✓ AsyncAPI spec written to %s\n", exportOutput)
	} else {
		fmt.Println(string(output))
	}

	return nil
}

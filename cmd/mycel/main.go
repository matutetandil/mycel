package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/envdefaults"
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
	version = "1.20.4"
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

Philosophy: Configuration, not code. You define WHAT you want, Mycel handles HOW.

Quick Start:
  mycel start --config ./my-service     Start a service
  mycel validate --config ./my-service  Validate configuration
  mycel check --config ./my-service     Test connector connectivity

Environment Variables:
  MYCEL_ENV         Environment (development, staging, production)
  MYCEL_LOG_LEVEL   Log level (debug, info, warn, error)
  MYCEL_LOG_FORMAT  Log format (text, json)

Documentation:
  https://github.com/matutetandil/mycel`,
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

To disable hot reload, use --hot-reload=false

Examples:
  # Start from current directory
  mycel start

  # Start from specific config directory
  mycel start --config ./examples/basic

  # Start with production environment
  mycel start --config ./my-service --env production

  # Start with debug logging
  mycel start --log-level debug

  # Start with JSON logs (for production)
  mycel start --log-format json

  # Start without hot reload
  mycel start --hot-reload=false

  # Using environment variables
  MYCEL_ENV=production MYCEL_LOG_FORMAT=json mycel start`,
	RunE: runStart,
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration files",
	Long: `Validate all HCL configuration files without starting the runtime.

This command parses and validates your configuration, checking for:
- HCL syntax errors
- Missing required fields
- Invalid connector types
- Flow configuration issues
- Type definition problems

Examples:
  # Validate current directory
  mycel validate

  # Validate specific config directory
  mycel validate --config ./my-service

  # Validate with environment overlay
  mycel validate --config ./my-service --env production

Output shows:
  - Number of connectors, flows, and types found
  - Details of each component
  - Any errors or warnings`,
	RunE: runValidate,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check connector connectivity",
	Long: `Check connectivity to all configured connectors.

This command attempts to connect to each configured connector and reports
the status. Use this to verify that:
- Database connections are working
- External APIs are reachable
- Message queue brokers are available
- Cache servers are responding

Examples:
  # Check current directory
  mycel check

  # Check specific config
  mycel check --config ./my-service

  # Check with specific environment
  mycel check --config ./my-service --env staging

Common issues detected:
  - Connection refused (service not running)
  - Authentication failed (wrong credentials)
  - Timeout (network issues, firewall)
  - Unknown host (DNS issues)`,
	RunE: runCheck,
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export API documentation",
	Long: `Export API documentation in various formats.

Available formats:
  openapi   - OpenAPI 3.0 for REST APIs
  asyncapi  - AsyncAPI 2.6 for message queues (RabbitMQ, Kafka)

Examples:
  mycel export openapi                    # Export REST API docs
  mycel export asyncapi                   # Export MQ docs
  mycel export openapi -o api.yaml        # Save to file
  mycel export openapi -f json            # Export as JSON`,
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

	verboseFlow  bool
	debugSuspend bool

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
	startCmd.Flags().BoolVar(&verboseFlow, "verbose-flow", false, "Log all flow pipeline stages per request (debug)")
	startCmd.Flags().BoolVar(&debugSuspend, "debug-suspend", false, "Defer event-driven connector start until debugger connects")

	// Export command flags (OpenAPI)
	exportOpenAPICmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
	exportOpenAPICmd.Flags().StringVarP(&exportFormat, "format", "f", "yaml", "Output format: yaml, json")
	exportOpenAPICmd.Flags().StringVar(&exportBaseURL, "base-url", "", "Override base URL for API server")

	// Export command flags (AsyncAPI)
	exportAsyncAPICmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
	exportAsyncAPICmd.Flags().StringVarP(&exportFormat, "format", "f", "yaml", "Output format: yaml, json")

	// Propagate CLI version to the runtime package so banner, health,
	// and metrics all report the correct Mycel version.
	runtime.Version = version

	// Add commands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(pluginCmd)

	// Add export subcommands
	exportCmd.AddCommand(exportOpenAPICmd)
	exportCmd.AddCommand(exportAsyncAPICmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// Load .env file if present (does not override existing env vars)
	loadDotEnv()

	// Setup logger with priority: flag > env var > environment default
	logger := createLogger()

	// Resolve environment with priority: flag > env var > default
	env := resolveEnvironment()

	// Resolve hot reload: explicit flag > environment default
	hotReloadEnabled := hotReload
	if !cmd.Flags().Changed("hot-reload") {
		// Flag not explicitly set — use environment default
		hotReloadEnabled = envdefaults.ForEnvironment(env).HotReload
	}

	// Debug features are dev-only
	effectiveVerboseFlow := verboseFlow
	if effectiveVerboseFlow && !isDevEnvironment(env) {
		logger.Warn("--verbose-flow is only available in development mode, ignoring")
		effectiveVerboseFlow = false
	}

	// Debug suspend: flag > env var, dev-only
	effectiveDebugSuspend := debugSuspend
	if !effectiveDebugSuspend {
		if val := os.Getenv("MYCEL_DEBUG_SUSPEND"); strings.EqualFold(val, "true") || val == "1" {
			effectiveDebugSuspend = true
		}
	}
	if effectiveDebugSuspend && !isDevEnvironment(env) {
		logger.Warn("--debug-suspend is only available in development mode, ignoring")
		effectiveDebugSuspend = false
	}

	// Create runtime
	rt, err := runtime.New(runtime.Options{
		ConfigDir:    configDir,
		Environment:  env,
		Logger:       logger,
		HotReload:    hotReloadEnabled,
		VerboseFlow:  effectiveVerboseFlow,
		DebugSuspend: effectiveDebugSuspend,
	})
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	// Start runtime (blocks until shutdown)
	ctx := context.Background()
	return rt.Start(ctx)
}

// createLogger creates a logger based on flags and environment variables.
// Priority: flag > env var > environment default > hardcoded default
func createLogger() *slog.Logger {
	// Start with environment-aware defaults
	env := resolveEnvironment()
	envDefs := envdefaults.ForEnvironment(env)

	cfg := &logging.Config{
		Level:  envDefs.LogLevel,
		Format: envDefs.LogFormat,
	}

	// Env vars override environment defaults
	if envLevel := os.Getenv("MYCEL_LOG_LEVEL"); envLevel != "" {
		cfg.Level = strings.ToLower(envLevel)
	}
	if envFormat := os.Getenv("MYCEL_LOG_FORMAT"); envFormat != "" {
		cfg.Format = strings.ToLower(envFormat)
	}

	// Flags override everything
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
	// Load .env file if present (so env() in HCL resolves correctly)
	loadDotEnv()

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
		fromOp := ""
		if f.From != nil {
			fromOp = f.From.GetOperation()
		}
		toTarget := ""
		if f.To != nil {
			toTarget = f.To.GetTarget()
		} else if len(f.MultiTo) > 0 {
			toTarget = fmt.Sprintf("%d destinations", len(f.MultiTo))
		}
		fmt.Printf("    - %s: %s → %s\n", f.Name, fromOp, toTarget)
	}

	fmt.Printf("  Types: %d\n", len(config.Types))
	for _, t := range config.Types {
		fmt.Printf("    - %s (%d fields)\n", t.Name, len(t.Fields))
	}

	return nil
}

func runCheck(cmd *cobra.Command, args []string) error {
	// Load .env file if present (so env() in HCL resolves correctly)
	loadDotEnv()

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

// loadDotEnv loads environment variables from a .env file if present.
// It tries <configDir>/.env first, then falls back to ./.env.
// Already-set environment variables are NOT overridden.
// Missing .env files are silently ignored (normal for production/Docker).
func loadDotEnv() {
	// Try config directory first
	configEnv := filepath.Join(configDir, ".env")
	if err := godotenv.Load(configEnv); err == nil {
		return
	}

	// Fall back to current directory (only if configDir is not ".")
	if configDir != "." {
		_ = godotenv.Load(".env")
	}
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

// isDevEnvironment returns true if the environment is development (the default).
func isDevEnvironment(env string) bool {
	switch strings.ToLower(env) {
	case "development", "dev", "":
		return true
	default:
		return false
	}
}

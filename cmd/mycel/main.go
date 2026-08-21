package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"go.uber.org/automaxprocs/maxprocs"
	"golang.org/x/mod/module"

	"github.com/matutetandil/mycel/v2/internal/connector"
	graphqlconn "github.com/matutetandil/mycel/v2/internal/connector/graphql"
	"github.com/matutetandil/mycel/v2/internal/envdefaults"
	"github.com/matutetandil/mycel/v2/internal/export/asyncapi"
	"github.com/matutetandil/mycel/v2/internal/export/openapi"
	"github.com/matutetandil/mycel/v2/internal/logging"
	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/internal/runtime"
)

// Environment variable names
const (
	EnvEnvironment = "MYCEL_ENV"
)

var (
	// Version is the release this source tree belongs to. It is the fallback:
	// buildInfo() overrides both of these when the binary carries real build
	// metadata, which it does for `go install` (module version) and for any
	// build from a git checkout (VCS revision).
	version = "2.18.0"
	commit  = "dev"
)

// Runs before the init() further down, and before any command executes. It has
// to also rewrite rootCmd.Version: that field is a package-variable
// initializer, so it captured the fallback values before this ran, which left
// `mycel --version` reporting "dev" while `mycel version` reported the real
// revision.
func init() {
	version, commit = buildInfo(version, commit)
	rootCmd.Version = fmt.Sprintf("%s (commit: %s)", version, commit)
}

// buildInfo reads the version and revision Go embeds in every binary.
//
// Nothing sets these through ldflags, so before this the commit was reported
// as "dev" everywhere — including released images — and a `go install` binary
// claimed whatever version happened to be hardcoded in the source at that tag.
// Go records both already: the module version for `go install pkg@version`,
// and the VCS revision plus a dirty flag for a build from a checkout.
//
// Falls back to the passed-in values when the binary has no build info, so
// `go run` and tests keep working.
func buildInfo(defVersion, defCommit string) (string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return defVersion, defCommit
	}

	v := defVersion
	// Only a real tagged release is a better answer than the source constant.
	// "(devel)" is a build with no module version at all, and a pseudo-version
	// (v2.13.1-0.20260807182040-21048d4ff956) is Go's synthetic stand-in for an
	// untagged commit — accurate, but it would put a timestamp and a hash in
	// the startup banner of every local build. The revision is reported
	// separately as the commit, so nothing is lost by preferring the constant.
	if mv := info.Main.Version; mv != "" && mv != "(devel)" && !module.IsPseudoVersion(mv) {
		v = strings.TrimPrefix(mv, "v")
	}

	c := defCommit
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				c = s.Value[:7]
			} else if s.Value != "" {
				c = s.Value
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty && c != defCommit {
		c += "-dirty"
	}
	return v, c
}

func main() {
	// Cobra has already reported the error; just set the exit status.
	if err := rootCmd.Execute(); err != nil {
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
  MYCEL_ENV          Environment (development, staging, production)
  MYCEL_LOG_LEVEL    Log level (debug, info, warn, error)
  MYCEL_LOG_FORMAT   Log format (text, json)
  MYCEL_PAYLOAD_SHOW Log incoming flow payloads (requires debug level)
  MYCEL_PAYLOAD_SIZE Max logged payload bytes (e.g. 512, 4k, 1m; default 4k)
  MYCEL_TRACING      Enable OpenTelemetry tracing (also auto-on when
                     OTEL_EXPORTER_OTLP_ENDPOINT is set; honors OTEL_* vars)

Documentation:
  https://github.com/matutetandil/mycel`,
	Version: fmt.Sprintf("%s (commit: %s)", version, commit),

	// Suppress the usage dump once a command is actually running: a config that
	// fails to validate is not a usage mistake, and burying the diagnostics
	// under the flag list helps nobody. Flag and argument errors are raised
	// before this hook runs, so those still print usage.
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		cmd.SilenceUsage = true
	},
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Mycel version",
	Long: `Print the Mycel version, build commit, Go toolchain and platform.

The same information appears in the startup banner, but on a pod that has been
running for a while that line has long rolled out of the log buffer. This
command answers "what is actually running here?" without a restart.

Examples:
  mycel version`,
	RunE: runVersion,
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

var exportGraphQLCmd = &cobra.Command{
	Use:   "graphql-schema",
	Short: "Export the GraphQL schema (SDL)",
	Long: `Export the GraphQL schema your configuration describes, as SDL.

Types and their inputs come from the type blocks. Query and Mutation fields
come from the flows that serve them, since a field exists exactly when a flow
answers it — operation = "Query.users" is the declaration. A flow's validate
block names the argument and result types; without one the field is JSON.

This reads the configuration rather than a running service, which is what a
federation gateway needs at build time.

Examples:
  mycel export graphql-schema                       # Output to stdout
  mycel export graphql-schema -o schema.graphql     # Write to file`,
	RunE: runExportGraphQLSchema,
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
	mockOnly     []string
	noMock       []string

	// Check flags
	checkTimeout time.Duration

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
	// The runtime and the parser have always handled these; the flags were
	// documented in four places and never registered, so following the
	// documentation produced "unknown flag: --mock". Repeatable and
	// comma-separated both work, because both spellings are documented.
	startCmd.Flags().StringSliceVar(&mockOnly, "mock", nil, "Mock these connectors, or \"all\" (repeatable, or comma-separated)")
	startCmd.Flags().StringSliceVar(&noMock, "no-mock", nil, "Do not mock these connectors, or \"all\" (repeatable, or comma-separated)")

	// Check command flags
	checkCmd.Flags().DurationVar(&checkTimeout, "timeout", runtime.DefaultConnectivityTimeout,
		"Per-connector timeout for the connectivity check")

	// Export command flags (OpenAPI)
	exportOpenAPICmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
	exportOpenAPICmd.Flags().StringVarP(&exportFormat, "format", "f", "yaml", "Output format: yaml, json")
	exportOpenAPICmd.Flags().StringVar(&exportBaseURL, "base-url", "", "Override base URL for API server")

	// Export command flags (AsyncAPI)
	exportAsyncAPICmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
	exportAsyncAPICmd.Flags().StringVarP(&exportFormat, "format", "f", "yaml", "Output format: yaml, json")

	// SDL has one serialisation, so there is no format flag to offer here.
	exportGraphQLCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")

	// Propagate CLI version to the runtime package so banner, health,
	// and metrics all report the correct Mycel version.
	runtime.Version = version

	// Add commands
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(pluginCmd)

	// Add subcommands
	addCmd.AddCommand(addConnectorCmd)
	addCmd.AddCommand(addFlowCmd)
	addCmd.AddCommand(addAspectCmd)
	addCmd.AddCommand(addTypeCmd)
	addConnectorCmd.Flags().StringVar(&addType, "type", "", "Connector type (see --list)")
	addConnectorCmd.Flags().StringVar(&addDriver, "driver", "", "Driver, for types that have one")
	addConnectorCmd.Flags().BoolVar(&addListTypes, "list", false, "List available connector types")
	addFlowCmd.Flags().StringVar(&addFrom, "from", "", "Source connector")
	addFlowCmd.Flags().StringVar(&addTo, "to", "", "Destination connector")
	addFlowCmd.Flags().StringVar(&addOperation, "operation", "", "Source operation (e.g. \"GET /orders\")")
	addFlowCmd.Flags().StringVar(&addTarget, "target", "", "Destination target (e.g. a table name)")
	addAspectCmd.Flags().StringVar(&addOn, "on", "", "Flow name patterns, comma-separated")
	addAspectCmd.Flags().StringVar(&addWhen, "when", "", "When to execute (before, after, around, on_error, on_drop)")
	addAspectCmd.Flags().StringVar(&addActionConnector, "action-connector", "", "Connector the action calls")
	addAspectCmd.Flags().StringVar(&addActionFlow, "action-flow", "", "Flow the action invokes")
	addTypeCmd.Flags().StringVar(&addFields, "fields", "", "Fields as name:type[:format], comma-separated")

	// Add export subcommands
	exportCmd.AddCommand(exportOpenAPICmd)
	exportCmd.AddCommand(exportAsyncAPICmd)
	exportCmd.AddCommand(exportGraphQLCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// Load .env file if present (does not override existing env vars)
	loadDotEnv()

	// Setup logger with priority: flag > env var > environment default
	logger := createLogger()

	// Align GOMAXPROCS with the CPU cgroup quota (Kubernetes, Docker). Without
	// this the Go runtime sizes its scheduler to the host's core count, so a
	// container limited to a fraction of a CPU spins up far more OS threads
	// than it can run, wasting context switches and throttling the GC.
	if _, err := maxprocs.Set(maxprocs.Logger(func(format string, args ...interface{}) {
		logger.Info(fmt.Sprintf(format, args...))
	})); err != nil {
		logger.Warn("could not adjust GOMAXPROCS from cgroup quota", "error", err)
	}

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

	// Incoming-payload debug logging (MYCEL_PAYLOAD_SHOW / MYCEL_PAYLOAD_SIZE).
	// Payloads only emit at debug level, so warn if opted in without it.
	payloadCfg := logging.PayloadLogFromEnv()
	if payloadCfg.Show && !logger.Enabled(context.Background(), slog.LevelDebug) {
		logger.Warn("MYCEL_PAYLOAD_SHOW=true but log level is not debug; payloads will not be logged. Set MYCEL_LOG_LEVEL=debug.")
	}

	// Create runtime
	rt, err := runtime.New(runtime.Options{
		ConfigDir:        configDir,
		Environment:      env,
		Logger:           logger,
		HotReload:        hotReloadEnabled,
		VerboseFlow:      effectiveVerboseFlow,
		ShowPayload:      payloadCfg.Show,
		PayloadMaxBytes:  payloadCfg.MaxBytes,
		DebugSuspend:     effectiveDebugSuspend,
		MockConnectors:   strings.Join(mockOnly, ","),
		NoMockConnectors: strings.Join(noMock, ","),
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

func runVersion(cmd *cobra.Command, args []string) error {
	fmt.Printf("mycel %s (commit: %s, %s, %s/%s)\n",
		version, commit, goruntime.Version(), goruntime.GOOS, goruntime.GOARCH)
	return nil
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Load .env file if present (so env() in HCL resolves correctly)
	loadDotEnv()

	fmt.Printf("Validating configuration...\n")
	fmt.Printf("  Config dir: %s\n", configDir)

	// Parse configuration
	schemaReg := runtime.NewSchemaRegistry()
	p := parser.NewHCLParserWithRegistry(schemaReg)
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Check every flow's "from" block against its source connector's schema
	if errs := runtime.ValidateFlowSchemas(config, schemaReg); len(errs) > 0 {
		fmt.Printf("\n✗ Configuration is invalid:\n\n")
		for _, e := range errs {
			fmt.Printf("    - %s\n", e)
		}
		fmt.Println()
		return fmt.Errorf("validation failed: %d flow error(s)", len(errs))
	}

	// A hook naming a flow that does not exist would otherwise surface as a
	// line in a log during whatever the hook was meant to catch.
	if errs := runtime.ValidateAuthHooks(config); len(errs) > 0 {
		fmt.Printf("\n✗ Configuration is invalid:\n\n")
		for _, e := range errs {
			fmt.Printf("    - %s\n", e)
		}
		fmt.Println()
		return fmt.Errorf("validation failed: %d auth hook error(s)", len(errs))
	}

	// And each connector's settings against the words that connector accepts,
	// so a misspelt auth type is caught here rather than by whoever wonders
	// why every request comes back unauthorised.
	if errs := runtime.ValidateConnectorSchemas(config, schemaReg); len(errs) > 0 {
		fmt.Printf("\n✗ Configuration is invalid:\n\n")
		for _, e := range errs {
			fmt.Printf("    - %s\n", e)
		}
		fmt.Println()
		return fmt.Errorf("validation failed: %d connector error(s)", len(errs))
	}

	// Aspects are checked with the same registry startup uses, so a config
	// cannot pass validate and then refuse to start.
	if err := runtime.ValidateAspects(config); err != nil {
		fmt.Printf("\n✗ Configuration is invalid:\n\n    - %s\n\n", err)
		return fmt.Errorf("validation failed: %w", err)
	}

	// Attributes that parse but do nothing. Inert, so not a failure, but the
	// config gives no hint that they are inert.
	if warnings := runtime.InertFlowAttrs(config); len(warnings) > 0 {
		fmt.Printf("\n⚠ Configuration with no effect (%d):\n\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("    - %s\n", w)
		}
	}

	// Unset env() references are not a validation failure — `mycel validate`
	// legitimately runs in CI, where production variables are absent. But the
	// config alone cannot tell you that an attribute silently resolved to "",
	// so report them rather than letting the run look entirely clean.
	printMissingEnvWarnings(config.Connectors)

	// Layout advice. Authoring-time only — it never reaches `mycel start`,
	// where an opinion about file organisation would be noise on a restart.
	if advice := runtime.LayoutAdvice(config); len(advice) > 0 {
		fmt.Printf("\n○ Readability (nothing is wrong):\n\n")
		for _, a := range advice {
			fmt.Printf("    - %s\n", a)
		}
		fmt.Printf("\n  Mycel merges every .mycel file, so this changes nothing at runtime.\n")
		fmt.Printf("  `mycel add connector <name>` and `mycel add flow <name>` place new\n")
		fmt.Printf("  declarations in their own file.\n")
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

	// The rest, listed only when present. A validate that acknowledges the
	// connectors and flows but says nothing about the saga just added reads as
	// though the file was not picked up.
	for _, s := range config.Sagas {
		if s == nil {
			continue
		}
		trigger := "no from block — it will not run"
		if s.From != nil {
			trigger = s.From.Connector
		}
		fmt.Printf("  Saga: %s (%d steps, from %s)\n", s.Name, len(s.Steps), trigger)
	}
	for _, m := range config.StateMachines {
		if m != nil {
			fmt.Printf("  State machine: %s (%d states, initial %s)\n", m.Name, len(m.States), m.Initial)
		}
	}
	for _, v := range config.Validators {
		if v != nil {
			fmt.Printf("  Validator: %s (%s)\n", v.Name, v.Type)
		}
	}
	for _, tr := range config.Transforms {
		if tr != nil {
			fmt.Printf("  Transform: %s (%d mappings)\n", tr.Name, len(tr.Mappings))
		}
	}
	if len(config.Constants) > 0 {
		names := make([]string, 0, len(config.Constants))
		for name := range config.Constants {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Printf("  Constants: %d\n", len(names))
		for _, name := range names {
			fmt.Printf("    - %s = %v\n", name, config.Constants[name])
		}
	}

	return nil
}

// printMissingEnvWarnings reports env() references that resolved to an empty
// string because the variable is unset. The parser already collected these per
// connector for the startup hint; surfacing them here means `mycel validate`
// stops looking clean on a config that cannot actually start.
func printMissingEnvWarnings(connectors []*connector.Config) {
	type ref struct{ conn, attr string }
	byVar := map[string][]ref{}
	var order []string

	for _, c := range connectors {
		for _, m := range c.MissingEnv {
			if _, seen := byVar[m.Name]; !seen {
				order = append(order, m.Name)
			}
			byVar[m.Name] = append(byVar[m.Name], ref{c.Name, m.Attr})
		}
	}
	if len(order) == 0 {
		return
	}

	fmt.Printf("\n⚠ Unset environment variables (%d):\n\n", len(order))
	for _, name := range order {
		for _, r := range byVar[name] {
			fmt.Printf("    - %s → connector %q (%s) resolves to \"\"\n", name, r.conn, r.attr)
		}
	}
	fmt.Printf("\n  These are only a warning here: validate does not need the deployment\n")
	fmt.Printf("  environment. Startup fails if the empty value leaves a required\n")
	fmt.Printf("  attribute unset. Give env() a default — env(\"NAME\", \"fallback\") —\n")
	fmt.Printf("  when an empty value is intended.\n")
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

	results := rt.CheckConnectivity(context.Background(), checkTimeout)
	// Shut down after the report is printed: Shutdown writes its own banner,
	// which would otherwise land in the middle of the results.
	defer func() { _ = rt.Shutdown() }()

	if len(results) == 0 {
		fmt.Printf("\n  No connectors configured.\n")
		return nil
	}

	fmt.Println()
	failed, inbound := 0, 0
	for _, res := range results {
		label := res.Name
		if desc := describeConnectorKind(res.Type, res.Driver); desc != "" {
			label = fmt.Sprintf("%s (%s)", res.Name, desc)
		}

		// A listener has no endpoint to reach. Saying so beats both a green
		// tick it did not earn and a cross for a check that never applied.
		if res.Inbound {
			inbound++
			fmt.Printf("  – %s: listens, nothing to reach\n", label)
			continue
		}

		if res.OK() {
			fmt.Printf("  ✓ %s: connected in %s\n", label, res.Duration.Round(time.Millisecond))
			continue
		}

		failed++
		if res.TimedOut {
			// No answer at all points at a firewall or a wrong host, where a
			// refusal points at a wrong port or a service that is down.
			fmt.Printf("  ✗ %s: no response within %s\n", label, checkTimeout)
			continue
		}
		fmt.Printf("  ✗ %s: %v\n", label, res.Err)
	}

	fmt.Println()
	if failed > 0 {
		return fmt.Errorf("%d of %d connectors unreachable", failed, len(results))
	}
	// Count only what was actually reached, so a config that is all listeners
	// does not claim to have verified anything.
	dialed := len(results) - inbound
	switch {
	case dialed == 0:
		fmt.Printf("✓ Nothing to reach: all %d connectors listen for inbound traffic.\n", inbound)
	case inbound > 0:
		fmt.Printf("✓ All %d reachable connectors are up (%d listen for inbound traffic).\n", dialed, inbound)
	default:
		fmt.Printf("✓ All %d connectors reachable!\n", dialed)
	}

	return nil
}

// describeConnectorKind renders "type/driver", or just the type when the
// connector has no driver.
func describeConnectorKind(connType, driver string) string {
	switch {
	case connType == "":
		return ""
	case driver == "":
		return connType
	default:
		return connType + "/" + driver
	}
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

func runExportGraphQLSchema(cmd *cobra.Command, args []string) error {
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// A connector that declares a schema file is served that file, so that file
	// is the schema. Rebuilding one from the type blocks would export something
	// the running service does not serve.
	sdl, err := declaredSchema(config)
	if err != nil {
		return err
	}
	if sdl == "" {
		sdl = graphqlconn.ExportSDL(config.Types, config.Flows)
	}

	if exportOutput != "" {
		if err := os.WriteFile(exportOutput, []byte(sdl), 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("✓ GraphQL schema written to %s\n", exportOutput)
		return nil
	}
	fmt.Print(sdl)
	return nil
}

// declaredSchema returns the SDL a graphql connector points at, or empty when
// none does.
//
// The path is resolved against the configuration directory, which is where a
// running service resolves it from.
func declaredSchema(config *parser.Configuration) (string, error) {
	for _, conn := range config.Connectors {
		if conn.Type != "graphql" {
			continue
		}
		schemaCfg, ok := conn.Properties["schema"].(map[string]interface{})
		if !ok {
			continue
		}
		path, ok := schemaCfg["path"].(string)
		if !ok || path == "" {
			continue
		}

		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("connector %q declares a schema at %s: %w", conn.Name, path, err)
		}
		return string(content), nil
	}
	return "", nil
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

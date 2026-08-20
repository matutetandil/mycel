// Package runtime provides the core runtime for Mycel services.
// It orchestrates configuration parsing, connector initialization,
// flow registration, and HTTP server lifecycle.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/matutetandil/mycel/v2/internal/aspect"
	"github.com/matutetandil/mycel/v2/internal/auth"
	"github.com/matutetandil/mycel/v2/internal/banner"
	"github.com/matutetandil/mycel/v2/internal/codec"
	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/connector/cache"
	conncdc "github.com/matutetandil/mycel/v2/internal/connector/cdc"
	"github.com/matutetandil/mycel/v2/internal/connector/database/mongodb"
	"github.com/matutetandil/mycel/v2/internal/connector/database/mysql"
	"github.com/matutetandil/mycel/v2/internal/connector/database/postgres"
	"github.com/matutetandil/mycel/v2/internal/connector/database/sqlite"
	"github.com/matutetandil/mycel/v2/internal/connector/discord"
	connelastic "github.com/matutetandil/mycel/v2/internal/connector/elasticsearch"
	"github.com/matutetandil/mycel/v2/internal/connector/email"
	"github.com/matutetandil/mycel/v2/internal/connector/exec"
	"github.com/matutetandil/mycel/v2/internal/connector/file"
	connftp "github.com/matutetandil/mycel/v2/internal/connector/ftp"
	"github.com/matutetandil/mycel/v2/internal/connector/graphql"
	conngrpc "github.com/matutetandil/mycel/v2/internal/connector/grpc"
	connhttp "github.com/matutetandil/mycel/v2/internal/connector/http"
	"github.com/matutetandil/mycel/v2/internal/connector/mq"
	connmqtt "github.com/matutetandil/mycel/v2/internal/connector/mqtt"
	connoauth "github.com/matutetandil/mycel/v2/internal/connector/oauth"
	connpdf "github.com/matutetandil/mycel/v2/internal/connector/pdf"
	"github.com/matutetandil/mycel/v2/internal/connector/profile"
	"github.com/matutetandil/mycel/v2/internal/connector/push"
	"github.com/matutetandil/mycel/v2/internal/connector/rest"
	conns3 "github.com/matutetandil/mycel/v2/internal/connector/s3"
	"github.com/matutetandil/mycel/v2/internal/connector/slack"
	"github.com/matutetandil/mycel/v2/internal/connector/sms"
	connsoap "github.com/matutetandil/mycel/v2/internal/connector/soap"
	connsse "github.com/matutetandil/mycel/v2/internal/connector/sse"
	"github.com/matutetandil/mycel/v2/internal/connector/tcp"
	"github.com/matutetandil/mycel/v2/internal/connector/webhook"
	connws "github.com/matutetandil/mycel/v2/internal/connector/websocket"
	"github.com/matutetandil/mycel/v2/internal/debug"
	"github.com/matutetandil/mycel/v2/internal/envdefaults"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/functions"
	"github.com/matutetandil/mycel/v2/internal/health"
	"github.com/matutetandil/mycel/v2/internal/hotreload"
	"github.com/matutetandil/mycel/v2/internal/metrics"
	"github.com/matutetandil/mycel/v2/internal/mock"
	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/internal/plugin"
	"github.com/matutetandil/mycel/v2/internal/ratelimit"
	"github.com/matutetandil/mycel/v2/internal/saga"
	"github.com/matutetandil/mycel/v2/internal/sanitize"
	"github.com/matutetandil/mycel/v2/internal/scheduler"
	"github.com/matutetandil/mycel/v2/internal/statemachine"
	msync "github.com/matutetandil/mycel/v2/internal/sync"
	"github.com/matutetandil/mycel/v2/internal/tracing"
	"github.com/matutetandil/mycel/v2/internal/transform"
	"github.com/matutetandil/mycel/v2/internal/validate"
	"github.com/matutetandil/mycel/v2/internal/validator"
	"github.com/matutetandil/mycel/v2/internal/workflow"
	"github.com/matutetandil/mycel/v2/pkg/schema"
	goredis "github.com/redis/go-redis/v9"
)

// Version is the current version of Mycel.
// Set from cmd/mycel/main.go at startup; defaults to "dev" for tests.
var Version = "dev"

// Runtime orchestrates the lifecycle of a Mycel service.
type Runtime struct {
	config            *parser.Configuration
	connectors        *connector.Registry
	operationResolver *connector.OperationResolver
	flows             *FlowRegistry
	transforms        map[string]*transform.Config
	types             map[string]*validate.TypeSchema
	namedCaches       map[string]*flow.NamedCacheConfig
	schemaRegistry    *schema.Registry
	health            *health.Manager
	metrics           *metrics.Registry
	rateLimiter       *ratelimit.Limiter
	logger            *slog.Logger
	environment       string
	configDir         string

	// Aspect-Oriented Programming (AOP) components
	aspectRegistry *aspect.Registry
	aspectExecutor *aspect.Executor

	// Mock system components
	mockManager *mock.Manager

	// WASM Functions registry for CEL extensions
	functionsRegistry *functions.Registry

	// Plugin registry for custom connectors and functions
	pluginRegistry *plugin.Registry

	// Auth manager for authentication system
	authManager *auth.Manager
	authHandler *auth.Handler
	authCleanup *auth.CleanupService

	// Sync manager for distributed locks, semaphores, and coordination
	syncManager *msync.Manager

	// State machine engine for state transitions
	stateMachineEngine *statemachine.Engine

	// Custom validator registry (WASM/regex/CEL validators)
	validatorRegistry *validator.Registry

	// Input sanitization pipeline (always active)
	sanitizer *sanitize.Pipeline

	// Workflow engine for long-running processes with persistence
	workflowEngine    *workflow.Engine
	workflowAPIServer *http.Server

	// Scheduler for cron-based flow triggers
	scheduler *scheduler.Scheduler

	// Verbose flow tracing (logs all pipeline stages per request)
	verboseFlow bool

	// Incoming payload debug logging (MYCEL_PAYLOAD_SHOW / MYCEL_PAYLOAD_SIZE)
	showPayload     bool
	payloadMaxBytes int

	// Hot reload components
	hotReloadEnabled bool
	hotReloader      *hotreload.Reloader
	hotWatcher       *hotreload.Watcher
	signalHandler    *hotreload.SignalHandler

	// Admin server for health/metrics when no REST connector is present
	adminServer *http.Server

	// OpenTelemetry tracer-provider shutdown (flushes buffered spans). No-op
	// when tracing is disabled. Set in New, invoked in Shutdown.
	traceShutdown func(context.Context) error

	// Debug protocol server for Mycel Studio IDE integration
	debugServer *debug.Server

	// Debug suspend: event-driven connectors defer Start() until a debugger connects
	debugSuspend      bool
	suspendedStarters []suspendedConnector

	// shutdownTimeout is the maximum time to wait for graceful shutdown.
	shutdownTimeout time.Duration

	// mu protects runtime state during hot reload
	mu sync.RWMutex
}

// suspendedConnector holds a connector whose Start() was deferred until a debugger connects.
type suspendedConnector struct {
	name    string
	starter Starter
}

// Options configures the runtime behavior.
type Options struct {
	// ConfigDir is the directory containing HCL configuration files.
	ConfigDir string

	// Environment is the deployment environment (dev, staging, prod).
	Environment string

	// Logger is the structured logger to use. If nil, a default is created.
	Logger *slog.Logger

	// ShutdownTimeout is the maximum time to wait for graceful shutdown.
	// Defaults to 30 seconds.
	ShutdownTimeout time.Duration

	// HotReload enables automatic configuration reload on file changes.
	// Like nginx, Mycel can reload configuration without restarting.
	HotReload bool

	// HotReloadDebounce is the debounce duration for hot reload.
	// Defaults to 500ms.
	HotReloadDebounce time.Duration

	// VerboseFlow enables per-request flow tracing via structured logs.
	// When true, every pipeline stage (sanitize, validate, transform, read/write)
	// is logged at debug level for all flows.
	VerboseFlow bool

	// ShowPayload logs the incoming payload of every flow at debug level,
	// regardless of source connector. Off by default (payloads may carry
	// PII/secrets); driven by MYCEL_PAYLOAD_SHOW.
	ShowPayload bool

	// PayloadMaxBytes caps how many bytes of the logged payload are emitted
	// before truncation. Driven by MYCEL_PAYLOAD_SIZE.
	PayloadMaxBytes int

	// MockConnectors is a comma-separated list of connectors to mock.
	// Empty means mock all when mocking is enabled.
	MockConnectors string

	// NoMockConnectors is a comma-separated list of connectors to exclude from mocking.
	NoMockConnectors string

	// DebugSuspend defers Start() on event-driven connectors until a debugger connects.
	// Only event-driven connectors (MQ, CDC, File watch, WebSocket, MQTT) are suspended;
	// request-response connectors (REST, gRPC, GraphQL, SOAP, TCP, SSE) start normally.
	DebugSuspend bool
}

// New creates a new runtime with the given options.
func New(opts Options) (*Runtime, error) {
	// Set defaults
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}
	if opts.ShutdownTimeout == 0 {
		opts.ShutdownTimeout = 30 * time.Second
	}

	// Build schema registry with all connector schemas
	schemaReg := NewSchemaRegistry()

	// Parse configuration
	p := parser.NewHCLParserWithRegistry(schemaReg)
	config, err := p.Parse(context.Background(), opts.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Everything that is wrong, rather than the first thing.
	//
	// Each of these used to return on its own, so a configuration with a bad
	// duration and a broken connector reference reported the duration, was
	// fixed, and reported the reference on the next start — which on a
	// deployment is a whole cycle per mistake. `mycel validate` reports them
	// together and so does this, from the same list, so the two cannot come to
	// disagree about what is checked.
	if errs := ValidateAll(config, schemaReg); len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}

	// Attributes that parse but do nothing. Not fatal — they are inert, and
	// failing here would break configs that work today — but the whole point
	// is that nobody can tell they are inert by reading the file.
	for _, w := range InertFlowAttrs(config) {
		opts.Logger.Warn("configuration has no effect", "detail", w)
	}

	// Create connector registry
	registry := connector.NewRegistry()

	// Create operation resolver for named operations
	opResolver := connector.NewOperationResolver()

	// Register built-in connector factories
	registerBuiltinFactories(registry, opts.Logger)

	env := opts.Environment
	if env == "" {
		env = "development"
	}

	// Build transforms map for fast lookup
	transforms := make(map[string]*transform.Config)
	for _, t := range config.Transforms {
		transforms[t.Name] = t
	}

	// Build types map for fast lookup
	types := make(map[string]*validate.TypeSchema)
	for _, t := range config.Types {
		types[t.Name] = t
	}

	// Build named caches map for fast lookup
	namedCaches := make(map[string]*flow.NamedCacheConfig)
	for _, c := range config.NamedCaches {
		namedCaches[c.Name] = c
	}

	// Create health manager
	healthMgr := health.NewManager(Version)

	// Create metrics registry
	serviceName := "mycel"
	serviceVersion := "0.0.0"
	if config.ServiceConfig != nil {
		if config.ServiceConfig.Name != "" {
			serviceName = config.ServiceConfig.Name
		}
		if config.ServiceConfig.Version != "" {
			serviceVersion = config.ServiceConfig.Version
		}
	}
	metricsReg := metrics.NewRegistry(serviceName, serviceVersion, Version, env)
	metrics.SetDefault(metricsReg)

	// Distributed tracing (OpenTelemetry). Opt-in via MYCEL_TRACING or an
	// OTEL_EXPORTER_OTLP_ENDPOINT; a no-op otherwise. The returned shutdown is
	// stored on the Runtime and invoked in Shutdown to flush buffered spans.
	traceShutdown, err := tracing.Setup(context.Background(), serviceName, serviceVersion)
	if err != nil {
		opts.Logger.Warn("failed to set up OpenTelemetry tracing; continuing without traces", "error", err)
		traceShutdown = func(context.Context) error { return nil }
	}

	// Create aspect registry and register aspects from config
	aspectReg := aspect.NewRegistry()
	if err := aspectReg.RegisterAll(config.Aspects); err != nil {
		return nil, fmt.Errorf("failed to register aspects: %w", err)
	}

	// Create mock manager
	mockCfg := config.MockConfig
	if mockCfg == nil {
		mockCfg = &mock.Config{}
	}
	// Apply CLI flags (override HCL config)
	parser.ParseMockFlags(opts.MockConnectors, opts.NoMockConnectors, mockCfg)
	mockMgr := mock.NewManager(mockCfg)

	// Create WASM functions registry and register functions from config
	functionsReg := functions.NewRegistry()
	for _, fnCfg := range config.Functions {
		if err := functionsReg.Register(fnCfg); err != nil {
			opts.Logger.Warn("failed to register WASM functions module",
				"module", fnCfg.Name,
				"wasm", fnCfg.WASM,
				"error", err.Error())
			// Continue - don't fail startup for optional WASM functions
		} else {
			opts.Logger.Info("registered WASM functions module",
				"module", fnCfg.Name,
				"exports", fnCfg.Exports)
		}
	}

	// Create plugin registry and load plugins
	pluginReg := plugin.NewRegistryWithLogger(opts.ConfigDir, opts.Logger)
	if len(config.Plugins) > 0 {
		if err := pluginReg.LoadAll(context.Background(), config.Plugins); err != nil {
			return nil, fmt.Errorf("failed to load plugins: %w", err)
		}

		// Register plugin connector factory with connector registry
		// This must be done BEFORE connectors are initialized
		registry.RegisterFactory(plugin.NewFactory(pluginReg))

		// Register plugin functions with the functions registry
		for pluginName, loadedPlugin := range pluginReg.GetFunctionsConfigs() {
			fnCfg := pluginReg.Loader().GetFunctionsConfig(loadedPlugin)
			if fnCfg != nil {
				if err := functionsReg.Register(fnCfg); err != nil {
					opts.Logger.Warn("failed to register plugin functions",
						"plugin", pluginName,
						"error", err.Error())
				} else {
					opts.Logger.Info("registered plugin functions",
						"plugin", pluginName,
						"exports", fnCfg.Exports)
				}
			}
		}

		opts.Logger.Info("plugins loaded",
			"count", len(config.Plugins))
	}

	// Initialize custom validator registry
	validatorReg := validator.NewRegistry()

	// Register validators from config
	for _, vCfg := range config.Validators {
		v, err := validator.CreateValidator(*vCfg)
		if err != nil {
			opts.Logger.Warn("failed to create validator",
				"validator", vCfg.Name,
				"type", string(vCfg.Type),
				"error", err.Error())
			continue
		}
		if err := validatorReg.Register(v); err != nil {
			opts.Logger.Warn("failed to register validator",
				"validator", vCfg.Name,
				"error", err.Error())
			continue
		}
		opts.Logger.Info("registered validator",
			"validator", vCfg.Name,
			"type", string(vCfg.Type))
	}

	// Register validators from plugins
	for pluginName, loadedPlugin := range pluginReg.Plugins() {
		if loadedPlugin.Manifest.Provides == nil {
			continue
		}
		for _, vp := range loadedPlugin.Manifest.Provides.Validators {
			wasmPath := filepath.Join(loadedPlugin.Path, vp.WASM)
			v, err := validator.NewWASMValidator(vp.Name, wasmPath, vp.Entrypoint, vp.Message)
			if err != nil {
				opts.Logger.Warn("failed to create plugin validator",
					"plugin", pluginName,
					"validator", vp.Name,
					"error", err.Error())
				continue
			}
			if err := validatorReg.Register(v); err != nil {
				opts.Logger.Warn("failed to register plugin validator",
					"plugin", pluginName,
					"validator", vp.Name,
					"error", err.Error())
				continue
			}
			opts.Logger.Info("registered plugin validator",
				"plugin", pluginName,
				"validator", vp.Name)
		}
	}

	// The auth manager is built in Start, once the connectors exist: its
	// storage may name one of them, and there is nothing to resolve against
	// here.
	var authMgr *auth.Manager
	var authHdl *auth.Handler

	// Create scheduler for cron-based flows
	sched := scheduler.New()

	// Initialize input sanitization pipeline (always active, cannot be disabled)
	sanitizeCfg := sanitize.DefaultConfig()
	if config.Security != nil {
		if config.Security.MaxInputLength > 0 {
			sanitizeCfg.MaxInputLength = config.Security.MaxInputLength
		}
		if config.Security.MaxFieldLength > 0 {
			sanitizeCfg.MaxFieldLength = config.Security.MaxFieldLength
		}
		if config.Security.MaxFieldDepth > 0 {
			sanitizeCfg.MaxFieldDepth = config.Security.MaxFieldDepth
		}
		if len(config.Security.AllowedControlChars) > 0 {
			sanitizeCfg.AllowedControlChars = sanitize.ParseAllowedControlChars(config.Security.AllowedControlChars)
		}
	}
	sanitizer := sanitize.NewPipeline(sanitizeCfg)

	// Load WASM sanitizers if configured
	if config.Security != nil {
		for _, ws := range config.Security.Sanitizers {
			entrypoint := ws.Entrypoint
			if entrypoint == "" {
				entrypoint = "sanitize"
			}
			wasmRule, err := sanitize.NewWASMRule(ws.Name, ws.WASM, entrypoint)
			if err != nil {
				opts.Logger.Warn("failed to load WASM sanitizer",
					"sanitizer", ws.Name,
					"wasm", ws.WASM,
					"error", err.Error())
				continue
			}
			sanitizer.AddRule(wasmRule)
			opts.Logger.Info("registered WASM sanitizer",
				"sanitizer", ws.Name)
		}
	}

	// Register sanitizers from plugins
	for pluginName, loadedPlugin := range pluginReg.Plugins() {
		if loadedPlugin.Manifest.Provides == nil {
			continue
		}
		for _, sp := range loadedPlugin.Manifest.Provides.Sanitizers {
			wasmPath := filepath.Join(loadedPlugin.Path, sp.WASM)
			entrypoint := sp.Entrypoint
			if entrypoint == "" {
				entrypoint = "sanitize"
			}
			wasmRule, err := sanitize.NewWASMRule(sp.Name, wasmPath, entrypoint)
			if err != nil {
				opts.Logger.Warn("failed to load plugin sanitizer",
					"plugin", pluginName,
					"sanitizer", sp.Name,
					"error", err.Error())
				continue
			}
			sanitizer.AddRule(wasmRule)
			opts.Logger.Info("registered plugin sanitizer",
				"plugin", pluginName,
				"sanitizer", sp.Name)
		}
	}

	r := &Runtime{
		config:            config,
		connectors:        registry,
		operationResolver: opResolver,
		flows:             NewFlowRegistry(),
		transforms:        transforms,
		types:             types,
		namedCaches:       namedCaches,
		schemaRegistry:    schemaReg,
		health:            healthMgr,
		metrics:           metricsReg,
		traceShutdown:     traceShutdown,
		aspectRegistry:    aspectReg,
		mockManager:       mockMgr,
		functionsRegistry: functionsReg,
		pluginRegistry:    pluginReg,
		validatorRegistry: validatorReg,
		authManager:       authMgr,
		authHandler:       authHdl,
		scheduler:         sched,
		sanitizer:         sanitizer,
		logger:            opts.Logger,
		environment:       env,
		configDir:         opts.ConfigDir,
		verboseFlow:       opts.VerboseFlow,
		showPayload:       opts.ShowPayload,
		payloadMaxBytes:   opts.PayloadMaxBytes,
		hotReloadEnabled:  opts.HotReload,
		debugSuspend:      opts.DebugSuspend,
		shutdownTimeout:   opts.ShutdownTimeout,
	}

	// Initialize debug server early so flow handlers can reference it
	r.debugServer = debug.NewServer(r, opts.Logger)

	// Wire debug throttling: when a debugger connects/disconnects,
	// toggle single-message processing on all event-driven connectors.
	r.debugServer.OnClientChange = func(hasClients bool) {
		for _, name := range r.connectors.List() {
			conn, err := r.connectors.Get(name)
			if err != nil {
				continue
			}
			if throttler, ok := conn.(connector.DebugThrottler); ok {
				throttler.SetDebugMode(hasClients)
			}
		}
	}

	// Wire debug.ready handshake: start suspended connectors only after
	// the IDE has finished setting breakpoints and sent debug.ready.
	// This eliminates the race condition where messages arrive before
	// breakpoints are configured.
	r.debugServer.OnReady = func() {
		if len(r.suspendedStarters) > 0 {
			for _, sc := range r.suspendedStarters {
				r.logger.Info("debug ready: starting connector",
					"connector", sc.name)
				if err := sc.starter.Start(context.Background()); err != nil {
					r.logger.Error("failed to start suspended connector",
						"connector", sc.name, "error", err)
					continue
				}
				// Re-apply debug mode AFTER Start() so it takes effect on the
				// newly created channel/consumer (prefetch=1 + gate enabled).
				conn, _ := r.connectors.Get(sc.name)
				if throttler, ok := conn.(connector.DebugThrottler); ok {
					throttler.SetDebugMode(true)
				}
			}
			r.suspendedStarters = nil
		}
	}

	return r, nil
}

// registerBuiltinFactories registers the built-in connector factories.
func registerBuiltinFactories(registry *connector.Registry, logger *slog.Logger) {
	// REST connector for exposing HTTP endpoints (server)
	registry.RegisterFactory(rest.NewFactory(logger))

	// HTTP connector for calling external APIs (client)
	registry.RegisterFactory(connhttp.NewFactory())

	// Database connectors (SQL)
	registry.RegisterFactory(sqlite.NewFactory(logger))
	registry.RegisterFactory(postgres.NewFactory())
	registry.RegisterFactory(mysql.NewFactory())

	// Database connectors (NoSQL)
	registry.RegisterFactory(mongodb.NewFactory())

	// TCP connector for raw TCP communication (server + client)
	registry.RegisterFactory(tcp.NewFactory(logger))

	// Message Queue connector (RabbitMQ, Kafka, etc.)
	registry.RegisterFactory(mq.NewFactory(logger))

	// Exec connector for executing external commands (local + SSH)
	registry.RegisterFactory(exec.NewFactory(logger))

	// GraphQL connector for exposing/consuming GraphQL APIs
	registry.RegisterFactory(graphql.NewFactory(logger))

	// gRPC connector for exposing/consuming gRPC services
	registry.RegisterFactory(conngrpc.NewFactory(logger))

	// File connector for reading/writing files
	registry.RegisterFactory(file.NewFactory())

	// S3 connector for AWS S3 / MinIO
	registry.RegisterFactory(conns3.NewFactory())

	// Cache connector (Redis, Memory)
	registry.RegisterFactory(cache.NewFactory())

	// Notification connectors
	registry.RegisterFactory(email.NewFactory())
	registry.RegisterFactory(slack.NewFactory())
	registry.RegisterFactory(discord.NewFactory())
	registry.RegisterFactory(sms.NewFactory())
	registry.RegisterFactory(push.NewFactory())
	registry.RegisterFactory(webhook.NewFactory())

	// WebSocket connector for real-time bidirectional communication
	registry.RegisterFactory(connws.NewFactory(logger))

	// CDC connector for Change Data Capture (PostgreSQL logical replication)
	registry.RegisterFactory(conncdc.NewFactory(logger))

	// SSE connector for unidirectional server-to-client push
	registry.RegisterFactory(connsse.NewFactory(logger))

	// Elasticsearch connector for full-text search and analytics
	registry.RegisterFactory(connelastic.NewFactory(logger))

	// OAuth connector for social login flows
	registry.RegisterFactory(connoauth.NewFactory(logger))

	// MQTT connector for IoT and messaging
	registry.RegisterFactory(connmqtt.NewFactory(logger))

	// SOAP connector for calling/exposing SOAP web services
	registry.RegisterFactory(connsoap.NewFactory(logger))

	// FTP/SFTP connector for remote file transfer
	registry.RegisterFactory(connftp.NewFactory(logger))

	// PDF connector for generating PDF documents from templates
	registry.RegisterFactory(connpdf.NewFactory())

	// Profile connector (must be registered last - uses other factories)
	registry.RegisterFactory(profile.NewFactory(registry))
}

// Start initializes all connectors, registers flows, and starts the HTTP server.
// It blocks until a shutdown signal is received or the context is cancelled.
func (r *Runtime) Start(ctx context.Context) error {
	// Print ASCII banner
	banner.Print(Version)

	// Print service info
	serviceName := "mycel-service"
	serviceVersion := "0.0.0"
	if r.config.ServiceConfig != nil {
		if r.config.ServiceConfig.Name != "" {
			serviceName = r.config.ServiceConfig.Name
		}
		if r.config.ServiceConfig.Version != "" {
			serviceVersion = r.config.ServiceConfig.Version
		}
	}
	banner.PrintServiceInfo(serviceName, serviceVersion, r.environment, r.getRESTPort())

	// Spell out which deliveries each flow will actually receive, before the
	// first message arrives. On a subscription source `operation` silently
	// filters, so a pattern matching nothing is indistinguishable from a
	// broker that is simply quiet. This reads configuration only, so it runs
	// before connecting: the dispatch shape is worth knowing even when the
	// broker is down and startup is about to fail.
	r.reportDispatch(r.logger, r.schemaRegistry)

	// Propagate service version to health responses
	r.health.SetServiceVersion(serviceVersion)

	// Apply environment-aware health detail mode
	envDefs := envdefaults.ForEnvironment(r.environment)
	r.health.SetDetailedMode(envDefs.DetailedHealth)

	// Print startup warnings for production environment
	r.printStartupWarnings()

	// And, in every environment, the settings a connector was given and does
	// not read: those are the same mistake on a laptop as in production, and
	// the symptom is a default quietly standing in for what was written.
	r.warnAboutUnreadAttributes(r.schemaRegistry)

	r.logger.Info("starting service",
		"service", serviceName,
		"version", serviceVersion,
		"environment", r.environment,
		"mycel_version", Version,
	)

	// Initialize rate limiter if configured
	r.initRateLimiter()

	// Initialize connectors
	if err := r.initConnectors(ctx); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to initialize connectors: %w", err)
	}

	// Upgrade rate limiter to Redis if configured (needs connectors)
	r.upgradeRateLimiterToRedis()

	// Initialize sync manager (needs connectors to be ready)
	r.syncManager = msync.NewManager()

	// Create aspect executor (needs connectors to be initialized)
	if err := r.initAspects(); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to initialize aspects: %w", err)
	}

	// Initialize state machine engine
	r.stateMachineEngine = statemachine.NewEngine(r.connectors)
	for _, sm := range r.config.StateMachines {
		r.stateMachineEngine.Register(sm)
	}

	// Build the auth system now that the connectors it may store into exist.
	if err := r.initAuth(ctx); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to initialize auth: %w", err)
	}

	// Register flows
	if err := r.registerFlows(); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to register flows: %w", err)
	}

	// Mount the auth endpoints and the inbound webhooks, after flows so that a
	// flow claiming one of those paths keeps it.
	r.wireAPIKeyValidators()
	r.mountAuthEndpoints()
	r.mountInboundWebhooks()

	// Wire flow invoker into aspect executor (allows aspects to invoke flows)
	if r.aspectExecutor != nil {
		r.aspectExecutor.SetFlowInvoker(r.flows)
	}

	// The workflow engine first: a saga handler is given the engine that
	// exists when it is registered, and a saga with a delay or an await step
	// is only a long-running workflow if it has one. Registering sagas first
	// handed every one of them a nil engine, so each ran straight through the
	// synchronous executor — never pausing, never persisted, never answering
	// with a workflow id, and leaving the endpoints that drive workflows with
	// nothing to serve.
	if err := r.initWorkflowEngine(ctx); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to initialize workflow engine: %w", err)
	}

	// Register sagas
	if err := r.registerSagas(); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to register sagas: %w", err)
	}

	// Start REST connectors (HTTP servers)
	if err := r.startServers(ctx); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to start servers: %w", err)
	}

	// Start the scheduler for cron-based flows
	if r.scheduler != nil {
		r.scheduler.Start()
		if entries := r.scheduler.Entries(); len(entries) > 0 {
			r.logger.Info("scheduler started", "scheduled_flows", len(entries))
		}
	}

	// Start background goroutine to update runtime metrics (uptime + goroutines)
	metricsCtx, metricsCancel := context.WithCancel(ctx)
	defer metricsCancel()
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.metrics.SetGoRoutines(goruntime.NumGoroutine())
				r.metrics.SetUptime(time.Since(startTime).Seconds())
				r.reportCacheSizes()
			case <-metricsCtx.Done():
				return
			}
		}
	}()

	banner.PrintReady()

	// Initialize hot reload if enabled
	if r.hotReloadEnabled {
		if err := r.initHotReload(ctx); err != nil {
			r.logger.Warn("hot reload initialization failed", "error", err)
		}
	}

	// Wait for shutdown signal
	return r.waitForShutdown(ctx)
}

// reportCacheSizes records how much each cache is holding.
//
// mycel_cache_size is in the documented list of metrics and nothing ever set
// it, so it was absent from /metrics for as long as it has existed. It is
// reported from the periodic sweep rather than per operation: the number is
// only interesting as a trend, and counting on every read would put work on
// the hot path to answer a question nobody asks that often.
//
// Only caches that can answer cheaply are asked. A cache holding its entries
// in memory knows the count; one backed by Redis would have to ask the server,
// and a shared Redis is not somewhere to send a periodic DBSIZE from every
// service that happens to use it.
func (r *Runtime) reportCacheSizes() {
	if r.metrics == nil || r.connectors == nil {
		return
	}
	for _, name := range r.connectors.Names() {
		conn, err := r.connectors.Get(name)
		if err != nil {
			continue
		}
		if counted, ok := conn.(interface{ Len() int }); ok {
			r.metrics.SetCacheSize(name, int64(counted.Len()))
		}
	}
}

// getConnectorType returns the type of a connector by name (e.g., "mq", "rest", "soap").
func (r *Runtime) getConnectorType(name string) string {
	for _, cfg := range r.config.Connectors {
		if cfg.Name == name {
			return cfg.Type
		}
	}
	return ""
}

// getRESTPort returns the port of the first REST connector, or 0 if none.
func (r *Runtime) getRESTPort() int {
	for _, cfg := range r.config.Connectors {
		if cfg.Type == "rest" {
			if port, ok := connector.IntFromPropsStrict(cfg.Properties, "port"); ok {
				return port
			}
		}
	}
	return 0
}

// printStartupWarnings logs warnings for unsafe configurations in production/staging.
func (r *Runtime) printStartupWarnings() {
	isProd := r.environment == "production" || r.environment == "prod"
	isStaging := r.environment == "staging" || r.environment == "stage"

	if !isProd && !isStaging {
		return
	}

	for _, cfg := range r.config.Connectors {
		// Warn about SQLite in production
		if cfg.Type == "database" && cfg.Driver == "sqlite" && isProd {
			r.logger.Warn("SQLite is not recommended for production use",
				"connector", cfg.Name,
				"suggestion", "consider PostgreSQL or MySQL")
		}
	}

	// Warn about missing auth in production
	if r.config.Auth == nil && isProd {
		r.logger.Warn("no authentication configured in production",
			"suggestion", "consider adding an auth block to secure your endpoints")
	}

	// And the one that undoes TLS while looking like it configures it.
	//
	// insecure_skip_verify does not relax certificate checking, it turns it
	// off: the connection is still encrypted and anyone who can answer for the
	// address can read it. It is the setting people reach for to get past a
	// self-signed certificate in development and the one they forget to take
	// out, and nothing said a word about it in production.
	for _, cfg := range r.config.Connectors {
		if !skipsCertificateVerification(cfg.Properties) {
			continue
		}
		r.logger.Warn("certificate verification is turned off",
			"connector", cfg.Name,
			"environment", r.environment,
			"detail", "the connection is encrypted and unauthenticated: anything that can answer for the address can read it",
			"suggestion", "name the certificate authority with ca_cert instead")
	}
}

// skipsCertificateVerification reports whether a connector's tls block turns
// verification off, whichever way it spells it.
func skipsCertificateVerification(props map[string]interface{}) bool {
	tls, ok := props["tls"].(map[string]interface{})
	if !ok {
		return false
	}
	for _, name := range []string{"insecure_skip_verify", "skip_verify", "insecure"} {
		if enabled, ok := tls[name].(bool); ok && enabled {
			return true
		}
	}
	return false
}

// InitForTrace partially initializes the runtime for trace mode.
// It initializes connectors and registers flows but does NOT start servers.
// Used by `mycel trace` to execute individual flows without a full startup.
func (r *Runtime) InitForTrace(ctx context.Context) error {
	// Initialize connectors (no server start)
	if err := r.initConnectors(ctx); err != nil {
		return fmt.Errorf("failed to initialize connectors: %w", err)
	}

	// Initialize sync manager
	r.syncManager = msync.NewManager()

	// Initialize aspects
	if err := r.initAspects(); err != nil {
		return fmt.Errorf("failed to initialize aspects: %w", err)
	}

	// Initialize state machine engine
	r.stateMachineEngine = statemachine.NewEngine(r.connectors)
	for _, sm := range r.config.StateMachines {
		r.stateMachineEngine.Register(sm)
	}

	// Register flows
	if err := r.registerFlows(); err != nil {
		return fmt.Errorf("failed to register flows: %w", err)
	}

	// Wire flow invoker into aspect executor
	if r.aspectExecutor != nil {
		r.aspectExecutor.SetFlowInvoker(r.flows)
	}

	return nil
}

// GetFlow retrieves a flow handler by name from the flow registry.
//
// The read lock is for the registry itself rather than for what is in it: a
// hot reload replaces the whole thing, under r.mu, while the debugger and the
// trace command are reading flows by name. The lock existed and only the
// writer honoured it.
func (r *Runtime) GetFlow(name string) (*FlowHandler, bool) {
	return r.flowRegistry().Get(name)
}

// ListFlows returns all registered flow names.
func (r *Runtime) ListFlows() []string {
	return r.flowRegistry().List()
}

// flowRegistry returns the registry currently in use, safely against a reload
// swapping it.
func (r *Runtime) flowRegistry() *FlowRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.flows
}

// GetFlowConfig returns a flow config by name (debug.RuntimeInspector).
func (r *Runtime) GetFlowConfig(name string) (*flow.Config, bool) {
	handler, ok := r.flowRegistry().Get(name)
	if !ok {
		return nil, false
	}
	return handler.Config, true
}

// ListConnectors returns all registered connector names (debug.RuntimeInspector).
func (r *Runtime) ListConnectors() []string {
	return r.connectors.List()
}

// GetConnectorConfig returns a connector config by name (debug.RuntimeInspector).
func (r *Runtime) GetConnectorConfig(name string) (*connector.Config, bool) {
	for _, cfg := range r.config.Connectors {
		if cfg.Name == name {
			return cfg, true
		}
	}
	return nil, false
}

// ListTypes returns all type schemas (debug.RuntimeInspector).
func (r *Runtime) ListTypes() []*validate.TypeSchema {
	schemas := make([]*validate.TypeSchema, 0, len(r.types))
	for _, schema := range r.types {
		schemas = append(schemas, schema)
	}
	return schemas
}

// ListTransforms returns all named transform configs (debug.RuntimeInspector).
func (r *Runtime) ListTransforms() []*transform.Config {
	configs := make([]*transform.Config, 0, len(r.transforms))
	for _, cfg := range r.transforms {
		configs = append(configs, cfg)
	}
	return configs
}

// GetCELTransformer returns a CEL transformer for expression evaluation (debug.RuntimeInspector).
func (r *Runtime) GetCELTransformer() *transform.CELTransformer {
	// Return the first flow handler's transformer, or create a new one
	flows := r.flowRegistry()
	for _, name := range flows.List() {
		if handler, ok := flows.Get(name); ok && handler.Transformer != nil {
			return handler.Transformer
		}
	}
	// Fallback: create a fresh transformer
	t, _ := transform.NewCELTransformer()
	return t
}

// ListEventSources returns capabilities of event-driven connectors (debug.RuntimeInspector).
func (r *Runtime) ListEventSources() []debug.SourceCapability {
	var sources []debug.SourceCapability
	for _, name := range r.connectors.List() {
		conn, err := r.connectors.Get(name)
		if err != nil {
			continue
		}
		// Only event-driven connectors (those implementing DebugThrottler)
		if _, isEventDriven := conn.(connector.DebugThrottler); !isEventDriven {
			continue
		}
		dt := conn.(connector.DebugThrottler)
		connType, source := dt.SourceInfo()
		cap := debug.SourceCapability{
			Connector:     name,
			Type:          connType,
			Source:        source,
			ManualConsume: true,
		}
		sources = append(sources, cap)
	}
	return sources
}

// ConsumeOne fetches a single message from the named connector (debug.RuntimeInspector).
func (r *Runtime) ConsumeOne(ctx context.Context, connectorName string) error {
	conn, err := r.connectors.Get(connectorName)
	if err != nil {
		return fmt.Errorf("connector %q not found: %w", connectorName, err)
	}
	dt, ok := conn.(connector.DebugThrottler)
	if !ok {
		return fmt.Errorf("connector %q does not support debug consume", connectorName)
	}
	dt.AllowOne()
	return nil
}

// GetDebugServer returns the debug protocol server for flow handler integration.
func (r *Runtime) GetDebugServer() *debug.Server {
	return r.debugServer
}

// initConnectors creates and connects all configured connectors.
func (r *Runtime) initConnectors(ctx context.Context) error {
	fmt.Println("    Connectors:")

	for _, cfg := range r.config.Connectors {
		// Propagate runtime environment to each connector config
		cfg.Environment = r.environment

		// Register connector config with operation resolver
		r.operationResolver.Register(cfg)

		if err := r.connectors.Register(ctx, cfg); err != nil {
			return fmt.Errorf("failed to register connector %s: %w%s", cfg.Name, err, missingEnvHint(cfg))
		}

		// Build details string based on connector type
		details := r.getConnectorDetails(cfg)
		banner.PrintConnector(cfg.Name, cfg.Type, details)
	}

	// Connect all connectors
	if err := r.connectors.ConnectAll(ctx); err != nil {
		return err
	}

	// Wrap connectors with mocks if enabled
	if r.mockManager.IsEnabled() {
		if err := r.mockManager.WrapRegistry(r.connectors); err != nil {
			return fmt.Errorf("failed to wrap connectors with mocks: %w", err)
		}
		banner.PrintMockInfo(r.mockManager.GetConfig())
	}

	// Register all connectors with health manager
	for _, name := range r.connectors.List() {
		conn, _ := r.connectors.Get(name)
		r.health.Register(conn)
	}

	return nil
}

// getConnectorDetails returns a details string for a connector.
func (r *Runtime) getConnectorDetails(cfg *connector.Config) string {
	switch cfg.Type {
	case "rest":
		if port, ok := cfg.Properties["port"]; ok {
			return fmt.Sprintf("listening on :%v", port)
		}
	case "database":
		if db, ok := cfg.Properties["database"]; ok {
			return fmt.Sprintf("→ %v", db)
		}
	case "http":
		if baseURL, ok := cfg.Properties["base_url"]; ok {
			return fmt.Sprintf("→ %v", baseURL)
		}
	case "tcp":
		host := "0.0.0.0"
		if h, ok := cfg.Properties["host"].(string); ok {
			host = h
		}
		port := connector.IntFromProps(cfg.Properties, "port", 9000)
		protocol := "json"
		if p, ok := cfg.Properties["protocol"].(string); ok {
			protocol = p
		}
		driver := cfg.Driver
		if driver == "" {
			driver = "server"
		}
		if driver == "server" {
			return fmt.Sprintf("listening on %s:%d [%s]", host, port, protocol)
		}
		return fmt.Sprintf("→ %s:%d [%s]", host, port, protocol)
	case "mq":
		driver := cfg.Driver
		if driver == "" {
			driver = "rabbitmq"
		}

		// Handle Kafka
		if driver == "kafka" {
			// Check if consumer
			if consumerCfg, ok := cfg.Properties["consumer"].(map[string]interface{}); ok {
				groupID := ""
				if g, ok := consumerCfg["group_id"].(string); ok {
					groupID = g
				}
				topics := "?"
				if t, ok := consumerCfg["topics"].([]interface{}); ok && len(t) > 0 {
					if topicStr, ok := t[0].(string); ok {
						topics = topicStr
						if len(t) > 1 {
							topics += fmt.Sprintf(" (+%d)", len(t)-1)
						}
					}
				}
				return fmt.Sprintf("consuming %s [group: %s]", topics, groupID)
			}
			// Check if producer
			if producerCfg, ok := cfg.Properties["producer"].(map[string]interface{}); ok {
				topic := ""
				if t, ok := producerCfg["topic"].(string); ok {
					topic = t
				}
				return fmt.Sprintf("producing to %s", topic)
			}
			// Default Kafka info
			brokers := "localhost:9092"
			if b, ok := cfg.Properties["brokers"].([]interface{}); ok && len(b) > 0 {
				if brokerStr, ok := b[0].(string); ok {
					brokers = brokerStr
				}
			}
			return fmt.Sprintf("→ %s [kafka]", brokers)
		}

		// Handle RabbitMQ
		host := "localhost"
		if h, ok := cfg.Properties["host"].(string); ok {
			host = h
		}
		port := connector.IntFromProps(cfg.Properties, "port", 5672)
		// Check if consumer or publisher
		if queueCfg, ok := cfg.Properties["queue"].(map[string]interface{}); ok {
			if queueName, ok := queueCfg["name"].(string); ok {
				return fmt.Sprintf("consuming from %s [%s]", queueName, driver)
			}
		}
		if pubCfg, ok := cfg.Properties["publisher"].(map[string]interface{}); ok {
			if exchange, ok := pubCfg["exchange"].(string); ok {
				return fmt.Sprintf("publishing to %s [%s]", exchange, driver)
			}
		}
		return fmt.Sprintf("→ %s:%d [%s]", host, port, driver)
	case "exec":
		cmd := ""
		if c, ok := cfg.Properties["command"].(string); ok {
			cmd = c
		}
		driver := cfg.Driver
		if driver == "" {
			driver = "local"
		}
		if driver == "ssh" {
			sshHost := ""
			if ssh, ok := cfg.Properties["ssh"].(map[string]interface{}); ok {
				if h, ok := ssh["host"].(string); ok {
					sshHost = h
				}
			}
			return fmt.Sprintf("→ ssh://%s [%s]", sshHost, cmd)
		}
		return fmt.Sprintf("→ %s [%s]", cmd, driver)
	case "graphql":
		driver := cfg.Driver
		if driver == "" {
			driver = "server"
		}
		if driver == "server" {
			host := getString(cfg.Properties, "host", "0.0.0.0")
			port := getInt(cfg.Properties, "port", 4000)
			endpoint := getString(cfg.Properties, "endpoint", "/graphql")
			playground := getBool(cfg.Properties, "playground", true)
			if playground {
				return fmt.Sprintf("listening on %s:%d%s [playground enabled]", host, port, endpoint)
			}
			return fmt.Sprintf("listening on %s:%d%s", host, port, endpoint)
		}
		// Client
		endpoint := getString(cfg.Properties, "endpoint", "")
		return fmt.Sprintf("→ %s", endpoint)
	case "cache":
		driver := cfg.Driver
		if driver == "" {
			driver = "memory"
		}
		if driver == "redis" {
			url := getString(cfg.Properties, "url", "redis://localhost:6379")
			prefix := getString(cfg.Properties, "prefix", "")
			if prefix != "" {
				return fmt.Sprintf("→ %s [prefix: %s]", url, prefix)
			}
			return fmt.Sprintf("→ %s", url)
		}
		// Memory cache
		maxItems := getInt(cfg.Properties, "max_items", 10000)
		eviction := getString(cfg.Properties, "eviction", "lru")
		return fmt.Sprintf("in-memory [%s, max: %d]", eviction, maxItems)
	case "profiled":
		// Show profile information
		if profileConfig, ok := cfg.Properties["_profiles"].(*profile.Config); ok {
			profileNames := profileConfig.ProfileNames()
			activeProfile := profileConfig.Default
			if profileConfig.Select != "" {
				activeProfile = fmt.Sprintf("${%s}", profileConfig.Select)
			}
			return fmt.Sprintf("profiles: %v [active: %s]", profileNames, activeProfile)
		}
		return "profiled connector"
	}
	return ""
}

// registerFlows builds flow handlers from configuration.
func (r *Runtime) registerFlows() error {
	// Named operations are folded into their inline form first, so everything
	// below — route registration, destinations, steps — sees one language.
	if err := r.resolveNamedOperations(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("    Flows:")

	for _, cfg := range r.config.Flows {
		// A scheduled flow has no source: the clock is what triggers it, which
		// is how the documentation describes `when` and how the scheduled
		// example is written. Reading through a nil `from` crashed the service
		// at startup — the example demonstrating the feature could not be
		// started, and `mycel validate` accepted it.
		var source connector.Connector
		var err error
		if cfg.From != nil && cfg.From.GetConnector() != "" {
			source, err = r.connectors.Get(cfg.From.GetConnector())
			if err != nil {
				return fmt.Errorf("flow %s: source connector not found: %w", cfg.Name, err)
			}
		} else if cfg.When == "" {
			// No source and no schedule: nothing would ever run it.
			return fmt.Errorf("flow %q has no from block and no when schedule, so nothing can trigger it", cfg.Name)
		}

		// Get destination connector (optional — flows without "to" are echo flows)
		var dest connector.Connector
		if cfg.To != nil && cfg.To.Connector != "" {
			dest, err = r.connectors.Get(cfg.To.Connector)
			if err != nil {
				return fmt.Errorf("flow %s: destination connector not found: %w", cfg.Name, err)
			}
		}

		// Validate connector-specific parameters if connectors implement validators.
		// Validators may set defaults (e.g., operation = "*") in ConnectorParams.
		if sv, ok := source.(connector.SourceValidator); ok && cfg.From != nil {
			if err := sv.ValidateSourceParams(cfg.From.ConnectorParams); err != nil {
				return fmt.Errorf("flow %s: source %s: %w", cfg.Name, cfg.From.GetConnector(), err)
			}
		}
		if dest != nil {
			if tv, ok := dest.(connector.TargetValidator); ok && cfg.To != nil {
				if err := tv.ValidateTargetParams(cfg.To.ConnectorParams); err != nil {
					return fmt.Errorf("flow %s: target %s: %w", cfg.Name, cfg.To.Connector, err)
				}
			}
		}

		// Register the flow
		handler := &FlowHandler{
			Config:             cfg,
			Source:             source,
			SourceType:         r.sourceTypeOf(cfg),
			Dest:               dest,
			NamedTransforms:    r.transforms,
			Types:              r.types,
			Connectors:         r.connectors,
			OperationResolver:  r.operationResolver,
			NamedCaches:        r.namedCaches,
			AspectExecutor:     r.aspectExecutor,
			FunctionsRegistry:  r.functionsRegistry,
			SyncManager:        r.syncManager,
			StateMachineEngine: r.stateMachineEngine,
			Sanitizer:          r.sanitizer,
			ValidatorRegistry:  r.validatorRegistry,
			Logger:             r.logger,
			VerboseFlow:        r.verboseFlow,
			ShowPayload:        r.showPayload,
			PayloadMaxBytes:    r.payloadMaxBytes,
			DebugServer:        r.debugServer,
		}

		// Resolve the dedupe cache connector once at registration so the
		// hot path does not pay a connector lookup per message. A
		// misconfigured reference here is a fatal startup error — silent
		// fallback would defeat the dedupe contract entirely.
		if cfg.Dedupe != nil {
			cacheConn, err := r.connectors.Get(cfg.Dedupe.Cache)
			if err != nil {
				return fmt.Errorf("flow %q: dedupe cache connector %q not found: %w", cfg.Name, cfg.Dedupe.Cache, err)
			}
			cacheImpl, ok := cacheConn.(cache.Cache)
			if !ok {
				return fmt.Errorf("flow %q: dedupe cache %q does not implement the cache interface (got %T)", cfg.Name, cfg.Dedupe.Cache, cacheConn)
			}
			handler.DedupeCache = cacheImpl
		}

		r.flows.Register(cfg.Name, handler)

		// Schedule flow if it has a cron/interval trigger
		if cfg.When != "" && cfg.When != "always" {
			flowHandler := handler
			schedCfg := &scheduler.ScheduleConfig{
				FlowName: cfg.Name,
				When:     cfg.When,
				Handler: func(ctx context.Context) error {
					_, err := flowHandler.HandleRequest(ctx, nil)
					return err
				},
			}
			if err := r.scheduler.Schedule(schedCfg); err != nil {
				r.logger.Warn("failed to schedule flow",
					"flow", cfg.Name,
					"when", cfg.When,
					"error", err)
			} else {
				r.logger.Info("flow scheduled",
					"flow", cfg.Name,
					"when", cfg.When)
			}
		}

		// If the to operation is a subscription, register the subscription field on the dest connector
		if cfg.To != nil && isSubscriptionTarget(cfg.To.GetOperation()) {
			if subReg, ok := dest.(SubscriptionRegistrar); ok {
				fieldName := strings.TrimPrefix(cfg.To.GetOperation(), "Subscription.")
				filter := cfg.To.GetFilter()
				if filter != "" {
					subReg.RegisterSubscriptionWithFilter(fieldName, cfg.Returns, filter)
				} else {
					subReg.RegisterSubscription(fieldName, cfg.Returns)
				}
			}
		}

		// Parse operation to get method and path. A scheduled flow has no
		// source to parse: what triggers it is the schedule, so that is what
		// the banner shows.
		var method, path string
		if cfg.From != nil {
			method, path = r.parseFlowOperation(cfg.From.GetConnector(), cfg.From.GetOperation())
		} else {
			method, path = "CRON", cfg.When
		}
		// "(echo)" is only true of a flow that has no destination and shapes
		// nothing: a fan-out writes to several places, and a response block
		// answers with something the flow computed. Reporting all three as an
		// echo made the banner misdescribe the very first flow `mycel init`
		// generates.
		target := "(echo)"
		switch {
		case cfg.To != nil:
			target = cfg.To.Connector + ":" + cfg.To.GetTarget()
			if isSubscriptionTarget(cfg.To.GetOperation()) {
				target = cfg.To.Connector + ":" + cfg.To.GetOperation()
			}
		case len(cfg.MultiTo) > 0:
			targets := make([]string, 0, len(cfg.MultiTo))
			for _, to := range cfg.MultiTo {
				if to != nil {
					targets = append(targets, to.Connector)
				}
			}
			target = strings.Join(targets, ", ")
		case cfg.Response != nil:
			target = "(response)"
		}
		banner.PrintFlow(method, path, target)
	}

	return nil
}

// parseFlowOperation parses a flow operation based on the connector type.
// It resolves named operations to their inline format for display.
func (r *Runtime) parseFlowOperation(connectorName, operation string) (method, path string) {
	// Resolve named operation
	if r.operationResolver != nil {
		resolved, err := r.operationResolver.Resolve(connectorName, operation)
		if err == nil {
			// Use resolved inline format
			operation = resolved.Inline
		}
	}

	// Check connector type
	for _, cfg := range r.config.Connectors {
		if cfg.Name == connectorName {
			switch cfg.Type {
			case "tcp":
				// For TCP, the operation is the message type
				return "TCP", operation
			case "mq":
				// For MQ, the operation is the routing key pattern
				return "MQ", operation
			case "graphql":
				// For GraphQL, the operation is "Query.field" or "Mutation.field"
				return "GQL", operation
			}
		}
	}

	// For REST connectors, parse "METHOD /path"
	return parseOperationString(operation)
}

// isSubscriptionTarget returns true if the operation targets a GraphQL subscription.
func isSubscriptionTarget(operation string) bool {
	return strings.HasPrefix(operation, "Subscription.")
}

// parseOperationString splits "METHOD /path" into method and path.
func parseOperationString(op string) (method, path string) {
	for i, c := range op {
		if c == ' ' {
			return op[:i], op[i+1:]
		}
	}
	return "GET", op
}

// startServers starts all connector servers.
func (r *Runtime) startServers(ctx context.Context) error {
	for _, name := range r.connectors.List() {
		conn, _ := r.connectors.Get(name)

		// Check if this is a startable connector
		if starter, ok := conn.(Starter); ok {
			// Set health manager if connector supports it
			if hr, ok := conn.(HealthRegistrar); ok {
				hr.SetHealthManager(r.health)
			}

			// Set metrics registry if connector supports it
			if mr, ok := conn.(MetricsRegistrar); ok {
				mr.SetMetrics(r.metrics)
			}

			// Set rate limiter if connector supports it
			if rlr, ok := conn.(RateLimitRegistrar); ok && r.rateLimiter != nil {
				rlr.SetRateLimiter(r.rateLimiter)
			}

			// Register flow handlers for this connector
			r.registerFlowHandlers(name, conn)

			// Debug suspend: defer Start() for event-driven connectors until a debugger connects.
			// Event-driven connectors implement DebugThrottler (MQ, CDC, File watch, WebSocket, MQTT).
			// Request-response connectors (REST, gRPC, GraphQL, SOAP, TCP, SSE) start normally.
			if r.debugSuspend {
				if _, isEventDriven := conn.(connector.DebugThrottler); isEventDriven {
					r.suspendedStarters = append(r.suspendedStarters, suspendedConnector{
						name:    name,
						starter: starter,
					})
					r.logger.Info("debug suspend: connector start deferred until debugger connects",
						"connector", name)
					continue
				}
			}

			// Start the server
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start %s: %w", name, err)
			}
		}
	}

	// Always start admin server (health, metrics, debug protocol)
	if err := r.startAdminServer(); err != nil {
		return fmt.Errorf("failed to start admin server: %w", err)
	}

	// And the workflow endpoints, on their own port, when the configuration
	// asks for them. They are not on the admin server: see workflow_api.go.
	if err := r.startWorkflowAPI(); err != nil {
		return fmt.Errorf("failed to start workflow api: %w", err)
	}

	// Mark service as ready after all servers are started
	r.health.SetReady(true)

	return nil
}

// initWorkflowEngine sets up the workflow engine for long-running processes.
// It connects to the configured database connector and creates the workflow table.
func (r *Runtime) initWorkflowEngine(ctx context.Context) error {
	if r.config.ServiceConfig == nil || r.config.ServiceConfig.Workflow == nil {
		return nil
	}

	wfCfg := r.config.ServiceConfig.Workflow

	// Get the database connector
	conn, err := r.connectors.Get(wfCfg.Storage)
	if err != nil {
		return fmt.Errorf("workflow storage connector %q not found: %w", wfCfg.Storage, err)
	}

	// Get *sql.DB from the connector
	dbAccessor, ok := conn.(connector.DBAccessor)
	if !ok {
		return fmt.Errorf("connector %q does not support DB access (must be sqlite, postgres, or mysql)", wfCfg.Storage)
	}

	db := dbAccessor.DB()
	if db == nil {
		return fmt.Errorf("connector %q returned nil DB", wfCfg.Storage)
	}

	// Determine SQL dialect
	var dialect workflow.Dialect
	switch conn.Type() {
	case "database":
		// Check the actual driver by connector name pattern or try ping
		// Use the connector's underlying type
		switch conn.(type) {
		case *sqlite.Connector:
			dialect = workflow.DialectSQLite
		case *postgres.Connector:
			dialect = workflow.DialectPostgres
		case *mysql.Connector:
			dialect = workflow.DialectMySQL
		default:
			dialect = workflow.DialectSQLite // fallback
		}
	default:
		return fmt.Errorf("connector %q type %q is not a SQL database", wfCfg.Storage, conn.Type())
	}

	// Create store
	store := workflow.NewSQLStore(db, dialect, wfCfg.Table)

	// Auto-create table
	if wfCfg.AutoCreate {
		if err := store.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("failed to create workflow table: %w", err)
		}
	}

	// Create saga executor for the workflow engine
	sagaExecutor := saga.NewExecutor(r.connectors)

	// Create and start engine
	engine := workflow.NewEngine(store, sagaExecutor, r.logger)

	// Register sagas that need persistence
	for _, cfg := range r.config.Sagas {
		if workflow.NeedsPersistence(cfg) {
			engine.RegisterSaga(cfg)
		}
	}

	if err := engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start workflow engine: %w", err)
	}

	r.workflowEngine = engine
	r.logger.Info("workflow engine started", "storage", wfCfg.Storage, "table", wfCfg.Table)

	return nil
}

// startAdminServer starts a lightweight HTTP server for health checks and metrics.
// This ensures health/metrics endpoints are always available, even without a REST connector.
func (r *Runtime) startAdminServer() error {
	port := 9090
	if r.config.ServiceConfig != nil && r.config.ServiceConfig.AdminPort > 0 {
		port = r.config.ServiceConfig.AdminPort
	}

	mux := http.NewServeMux()

	// Register health endpoints
	r.health.RegisterHandlers(mux)

	// Register metrics endpoint
	if r.metrics != nil {
		mux.Handle("/metrics", r.metrics.Handler())
	}

	// The workflow endpoints are deliberately not here: they wake and cancel
	// running workflows, and this port is the read-only one. See workflow_api.go.

	// Register debug protocol (Mycel Studio IDE)
	r.debugServer.RegisterHandlers(mux)

	// Optional pprof profiling endpoints under /debug/pprof/. Opt-in via
	// MYCEL_PPROF: off by default because pprof exposes runtime internals and
	// the profile/trace endpoints are CPU-heavy. The admin port is internal
	// (reachable via `kubectl port-forward`), so it's safe to enable in any
	// environment — including production — to diagnose a live process.
	pprofEnabled := false
	if registerPprofHandlers(mux) {
		pprofEnabled = true
		r.logger.Warn("pprof profiling enabled on admin server",
			"path", "/debug/pprof/",
			"toggle", "MYCEL_PPROF",
			"hint", "kubectl port-forward then: go tool pprof http://<host>:9090/debug/pprof/goroutine")
	}

	addr := fmt.Sprintf(":%d", port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("admin server failed to listen on %s: %w", addr, err)
	}

	r.adminServer = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := r.adminServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			r.logger.Error("admin server error", "error", err)
		}
	}()

	endpoints := []string{"/health", "/health/live", "/health/ready", "/metrics", "/debug"}
	if pprofEnabled {
		endpoints = append(endpoints, "/debug/pprof/")
	}
	r.logger.Info("admin server started", "port", port, "endpoints", endpoints)
	banner.PrintConnector("admin", "http", fmt.Sprintf("health + metrics + debug on :%d", port))

	return nil
}

// registerPprofHandlers mounts the net/http/pprof endpoints on the admin mux
// when MYCEL_PPROF is set to a truthy value. Returns whether they were mounted.
// They live under /debug/pprof/ (distinct from the Studio /debug WebSocket).
func registerPprofHandlers(mux *http.ServeMux) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MYCEL_PPROF"))) {
	case "1", "true", "yes", "on":
	default:
		return false
	}

	// pprof.Index also serves the named profiles (goroutine, heap, allocs,
	// block, mutex, threadcreate) at /debug/pprof/<name>.
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)
	return true
}

// registerWorkflowEndpoints adds workflow management endpoints to an HTTP mux.
func (r *Runtime) registerWorkflowEndpoints(mux *http.ServeMux) {
	if r.workflowEngine == nil {
		return
	}

	// GET /workflows/{id} — get workflow instance status
	mux.HandleFunc("GET /workflows/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		inst, err := r.workflowEngine.GetInstance(req.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inst)
	})

	// POST /workflows/{id}/signal/{event} — send signal to awaiting workflow
	mux.HandleFunc("POST /workflows/{id}/signal/{event}", func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		event := req.PathValue("event")

		// A body that is not JSON is refused rather than dropped: decoded and
		// discarded, a malformed payload woke the workflow with no data at all,
		// which sent it down a branch reading fields that were never there —
		// and the caller was told the signal had worked.
		var data map[string]interface{}
		if req.Body != nil {
			if err := json.NewDecoder(req.Body).Decode(&data); err != nil && !errors.Is(err, io.EOF) {
				http.Error(w, fmt.Sprintf("signal body is not JSON: %v", err), http.StatusBadRequest)
				return
			}
		}

		if err := r.workflowEngine.Signal(req.Context(), id, event, data); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "signaled",
			"id":     id,
			"event":  event,
		})
	})

	// POST /workflows/{id}/cancel — cancel active workflow
	mux.HandleFunc("POST /workflows/{id}/cancel", func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		if err := r.workflowEngine.Cancel(req.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "cancelled",
			"id":     id,
		})
	})
}

// registerFlowHandlers registers HTTP handlers for flows using this connector.
func (r *Runtime) registerFlowHandlers(connectorName string, conn connector.Connector) {
	router, ok := conn.(RouteRegistrar)
	if !ok {
		return
	}

	// Check if this connector supports loading HCL types (e.g., GraphQL server)
	if typeLoader, ok := conn.(HCLTypeLoader); ok && len(r.types) > 0 {
		if err := typeLoader.LoadHCLTypes(r.types); err != nil {
			r.logger.Error("failed to load HCL types",
				"connector", connectorName,
				"error", err,
			)
		}
	}

	// Check if this connector supports typed args registration (preferred)
	routerWithArgs, hasArgsSupport := conn.(RouteRegistrarWithArgs)

	// Check if this connector supports return type registration (fallback)
	routerWithReturnType, hasReturnTypeSupport := conn.(RouteRegistrarWithReturnType)

	// Find flows that use this connector as source
	for _, handler := range r.flows.handlers {
		if handler.Config.From.GetConnector() == connectorName {
			// Wrap handler with format context if flow declares a format
			requestHandler := handler.HandleRequest
			if handler.Config.From.GetFormat() != "" {
				fromFormat := handler.Config.From.GetFormat()
				origHandler := requestHandler
				requestHandler = func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
					return origHandler(codec.WithFormat(ctx, fromFormat), input)
				}
				// The wrapper above serves the flow. The connector reads the
				// request and writes the answer against the transport's own
				// context, which the wrapper cannot reach, so it is told
				// separately — without this a flow declaring xml answered in
				// JSON.
				if fr, ok := conn.(interface {
					SetOperationFormat(operation, format string)
				}); ok {
					fr.SetOperationFormat(handler.Config.From.GetOperation(), fromFormat)
				}
			}

			// If flow has a return type and connector supports typed args, use RegisterRouteWithArgs
			if hasArgsSupport && handler.Config.Returns != "" {
				args := inferArgsFromFlow(handler.Config)
				routerWithArgs.RegisterRouteWithArgs(
					handler.Config.From.GetOperation(),
					requestHandler,
					handler.Config.Returns,
					args,
				)
			} else if hasReturnTypeSupport && handler.Config.Returns != "" {
				// Fallback to return type only registration
				routerWithReturnType.RegisterRouteWithReturnType(
					handler.Config.From.GetOperation(),
					requestHandler,
					handler.Config.Returns,
				)
			} else {
				router.RegisterRoute(handler.Config.From.GetOperation(), requestHandler)
			}
		}
	}

	// Register federated entity resolvers
	r.registerEntityResolvers(connectorName, conn)

	// Register job status endpoint if any flow uses async
	r.registerJobStatusEndpoint(connectorName, conn)
}

// registerJobStatusEndpoint registers a GET /jobs/:job_id endpoint on REST connectors
// when any flow uses async execution. Returns job status from cache.
func (r *Runtime) registerJobStatusEndpoint(connectorName string, conn connector.Connector) {
	// Check if any flow for this connector uses async
	hasAsync := false
	for _, handler := range r.flows.handlers {
		if handler.Config.From != nil && handler.Config.Async != nil {
			fromConn := handler.Config.From.GetConnector()
			if fromConn == connectorName {
				hasAsync = true
				break
			}
		}
	}

	if !hasAsync {
		return
	}

	router, ok := conn.(RouteRegistrar)
	if !ok {
		return
	}

	// Find the async storage connector from the first async flow
	var storageName string
	for _, handler := range r.flows.handlers {
		if handler.Config.From != nil && handler.Config.Async != nil {
			fromConn := handler.Config.From.GetConnector()
			if fromConn == connectorName {
				storageName = handler.Config.Async.Storage
				break
			}
		}
	}

	connRegistry := r.connectors
	router.RegisterRoute("GET /jobs/:job_id", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		jobID, _ := input["job_id"].(string)
		if jobID == "" {
			return map[string]interface{}{
				"error":            "job_id is required",
				"http_status_code": 400,
			}, nil
		}

		storageConn, err := connRegistry.Get(storageName)
		if err != nil {
			return nil, fmt.Errorf("async storage not available: %w", err)
		}

		cacheStorage, ok := storageConn.(cache.Cache)
		if !ok {
			return nil, fmt.Errorf("async storage does not implement cache interface")
		}

		data, exists, err := cacheStorage.Get(ctx, "job:"+jobID)
		if err != nil || !exists {
			return map[string]interface{}{
				"error":            "job not found",
				"http_status_code": 404,
			}, nil
		}

		var result map[string]interface{}
		if jsonErr := json.Unmarshal(data, &result); jsonErr != nil {
			return nil, jsonErr
		}

		return result, nil
	})
}

// registerEntityResolvers registers Federation entity resolvers on a GraphQL connector.
// It scans flows with `entity` attribute and HCL types with `_key` to create entity resolvers.
func (r *Runtime) registerEntityResolvers(connectorName string, conn connector.Connector) {
	entityReg, ok := conn.(EntityRegistrar)
	if !ok || !entityReg.IsFederationEnabled() {
		return
	}

	// Collect explicit entity resolver flows (flows with entity = "TypeName")
	entityFlows := make(map[string]*FlowHandler)
	for _, handler := range r.flows.handlers {
		if handler.Config.Entity != "" {
			entityFlows[handler.Config.Entity] = handler
		}
	}

	// Register entities from types with _key
	for typeName, typeSchema := range r.types {
		if len(typeSchema.Keys) == 0 {
			continue
		}

		// Build entity keys from type schema
		keys := make([]graphql.EntityKey, 0, len(typeSchema.Keys))
		for _, keyFields := range typeSchema.Keys {
			keys = append(keys, graphql.EntityKey{
				Fields:     keyFields,
				Resolvable: true,
			})
		}

		// Check for explicit entity resolver flow
		if flowHandler, exists := entityFlows[typeName]; exists {
			handler := flowHandler // capture for closure
			entityReg.RegisterEntity(typeName, keys, func(ctx context.Context, representation map[string]interface{}) (interface{}, error) {
				return handler.HandleRequest(ctx, representation)
			})
			r.logger.Info("registered entity resolver (explicit flow)",
				"type", typeName,
				"connector", connectorName,
			)
			continue
		}

		// Try to find a matching Query flow that reads from a database
		// Look for flows like: from { connector.api = "Query.user" } to { connector.db = "users" }
		var resolverHandler *FlowHandler
		for _, handler := range r.flows.handlers {
			if handler.Config.From.GetConnector() != connectorName {
				continue
			}
			// Check if the flow returns this type
			if handler.Config.Returns == typeName || handler.Config.Returns == typeName+"!" {
				resolverHandler = handler
				break
			}
		}

		if resolverHandler != nil {
			handler := resolverHandler // capture for closure
			entityReg.RegisterEntity(typeName, keys, func(ctx context.Context, representation map[string]interface{}) (interface{}, error) {
				return handler.HandleRequest(ctx, representation)
			})
			r.logger.Info("registered entity resolver (auto-detected from flow)",
				"type", typeName,
				"connector", connectorName,
			)
		} else {
			// A type carrying _key is one this subgraph tells the gateway it
			// can resolve by reference. Without a resolver the gateway routes
			// those lookups here and gets nothing back, which reads as a null
			// in the composed graph rather than as an error anywhere — and
			// this was said at debug level, which is off.
			r.logger.Warn("a federated type has a key and nothing to resolve it with",
				"type", typeName,
				"connector", connectorName,
				"hint", fmt.Sprintf("give a flow entity = %q, or a returns of %q", typeName, typeName),
			)
		}
	}
}

// inferArgsFromFlow extracts GraphQL arguments from flow step params.
// It looks for expressions like "input.id", "input.name" in step params
// and creates typed argument definitions.
// For mutations with a returns type and no step-inferred args, it automatically
// creates a typed input argument using the returns type (e.g., returns = "user"
// generates input: UserInput instead of input: JSON).
func inferArgsFromFlow(cfg *flow.Config) []*ArgDef {
	args := make(map[string]*ArgDef) // Use map to deduplicate

	// Extract from step params
	for _, step := range cfg.Steps {
		for _, value := range step.GetParams() {
			extractInputArgs(value, args)
		}
	}

	// Whether a mutation takes a typed input object is decided by what the
	// steps gave, before anything is read from the destination: a mutation
	// whose destination names its columns as :placeholders would otherwise
	// publish one argument per column and lose the `input` object it declares.
	inferredFromSteps := len(args)

	// And from the destination's own query.
	//
	// A GraphQL query flow that fetches with `query = "... WHERE sku = :sku"`
	// named its parameter there and nowhere else, and only a step's params
	// were read — so the field was published taking no arguments at all, and
	// asking for `product(sku: "ABC-123")`, which is what the example's README
	// shows, was answered "Unknown argument sku".
	for _, to := range destinations(cfg) {
		for _, name := range namedParameters(to.GetQuery()) {
			if args[name] == nil {
				args[name] = &ArgDef{
					Name:        name,
					Type:        "string",
					Required:    false,
					Description: fmt.Sprintf("Argument %s (inferred from the destination query)", name),
				}
			}
		}
		for _, value := range to.GetQueryFilter() {
			extractInputArgs(value, args)
		}
	}

	// For mutations with a custom returns type and no step-inferred args,
	// use the returns type as a typed input argument.
	// This generates typed input objects (e.g., returns = "user" → input: userInput)
	// instead of generic JSON. Scalar types are excluded since they don't map
	// to meaningful input objects.
	if inferredFromSteps == 0 && cfg.Returns != "" && strings.HasPrefix(cfg.From.GetOperation(), "Mutation.") {
		returnsType := strings.TrimSuffix(strings.TrimSuffix(cfg.Returns, "[]"), "!")
		if !isScalarReturnType(returnsType) {
			return []*ArgDef{{
				Name: "input",
				Type: returnsType,
			}}
		}
	}

	// Sorted, because these are published in a schema: built by ranging over a
	// map the order changed on every start, so two replicas of the same service
	// printed different schemas and an exported schema file differed run to run.
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]*ArgDef, 0, len(args))
	for _, name := range names {
		result = append(result, args[name])
	}

	return result
}

// extractInputArgs collects the fields an expression reads off the input, so
// each becomes an argument the GraphQL field publishes.
//
// It used to look only at values that began with "input.", which meant a step
// doing anything at all with the field — lower(input.email), input.first + " " +
// input.last, input.limit ?? 25 — published no argument, and a client sending
// one was told it did not exist. Every occurrence is taken now, wherever in the
// expression it appears.
// destinations returns every destination a flow writes to or reads from.
func destinations(cfg *flow.Config) []*flow.ToConfig {
	var out []*flow.ToConfig
	if cfg.To != nil {
		out = append(out, cfg.To)
	}
	out = append(out, cfg.MultiTo...)
	return out
}

// namedParameters returns the :name placeholders a statement carries.
var namedParameter = regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`)

func namedParameters(query string) []string {
	if query == "" {
		return nil
	}
	var out []string
	for _, match := range namedParameter.FindAllStringSubmatch(query, -1) {
		out = append(out, match[1])
	}
	return out
}

func extractInputArgs(value interface{}, args map[string]*ArgDef) {
	switch v := value.(type) {
	case string:
		for _, field := range inputFieldsIn(v) {
			if args[field] == nil {
				args[field] = &ArgDef{
					Name:        field,
					Type:        "string", // Default to string, could be improved with type inference
					Required:    false,    // Don't make required by default
					Description: fmt.Sprintf("Argument %s (inferred from flow)", field),
				}
			}
		}
	case map[string]interface{}:
		for _, subVal := range v {
			extractInputArgs(subVal, args)
		}
	case []interface{}:
		for _, item := range v {
			extractInputArgs(item, args)
		}
	}
}

// inputFieldsIn returns the first identifier of every input.<field> reference
// in an expression. input.address.city yields "address": the caller passes the
// argument, not the path below it.
func inputFieldsIn(expr string) []string {
	const prefix = "input."

	var fields []string
	seen := make(map[string]bool)
	for i := 0; i+len(prefix) <= len(expr); {
		next := strings.Index(expr[i:], prefix)
		if next < 0 {
			break
		}
		start := i + next
		// "myinput.id" is not a reference to the input.
		if start > 0 && isIdentifierByte(expr[start-1]) {
			i = start + len(prefix)
			continue
		}

		rest := expr[start+len(prefix):]
		end := 0
		for end < len(rest) && isIdentifierByte(rest[end]) {
			end++
		}
		if field := rest[:end]; field != "" && !seen[field] {
			seen[field] = true
			fields = append(fields, field)
		}
		i = start + len(prefix) + end
	}
	return fields
}

func isIdentifierByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// isScalarReturnType checks if a return type name is a GraphQL scalar type.
// Scalar types don't have meaningful input object counterparts.
func isScalarReturnType(typeName string) bool {
	switch strings.ToLower(typeName) {
	case "string", "int", "integer", "float", "number", "boolean", "bool", "id", "json", "datetime", "date", "time":
		return true
	}
	return false
}

// waitForShutdown blocks until a shutdown signal is received.
func (r *Runtime) waitForShutdown(ctx context.Context) error {
	// Create signal channel
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal or context cancellation
	select {
	case <-sigChan:
		// Signal received
	case <-ctx.Done():
		// Context cancelled
	}

	return r.Shutdown()
}

// Shutdown gracefully shuts down the runtime.
//
// The shutdown timeout is a hard deadline, not a suggestion. It used to be
// passed down as a context and nothing enforced it: a connector whose Close
// ignored the context and blocked on the network kept the process alive
// indefinitely. That is fatal under an orchestrator — Kubernetes sends SIGTERM,
// waits terminationGracePeriodSeconds (30 by default) and then SIGKILLs, so a
// runtime that will not exit gets killed mid-flight on every rolling update,
// with consumers never cancelled and in-flight messages lost. Exiting late but
// under our own control beats being killed.
func (r *Runtime) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
	defer cancel()

	banner.PrintShutdown()

	done := make(chan error, 1)
	go func() { done <- r.shutdownSteps(ctx) }()

	select {
	case err := <-done:
		banner.PrintGoodbye()
		return err
	case <-ctx.Done():
		// Deliberately not an error: the shutdown was asked for and got as far
		// as it could. Saying so loudly matters more than the exit code, which
		// an orchestrator would read as a failed container.
		r.logger.Warn("graceful shutdown did not finish within the timeout, exiting anyway",
			"timeout", r.shutdownTimeout)
		banner.PrintGoodbye()
		return nil
	}
}

// shutdownSteps performs the shutdown itself. It runs under the deadline its
// caller owns, so every step here may block without risking the process.
func (r *Runtime) shutdownSteps(ctx context.Context) error {
	// Mark service as not ready (stop accepting new traffic)
	r.health.SetReady(false)

	// Stop the scheduler, but do not wait past the deadline for a cron job
	// that is still running.
	if r.scheduler != nil {
		select {
		case <-r.scheduler.Stop().Done():
		case <-ctx.Done():
			r.logger.Warn("scheduler still had jobs running at shutdown")
		}
	}

	// Stop the auth cleanup loop
	if r.authCleanup != nil {
		if err := r.authCleanup.Stop(); err != nil {
			r.logger.Warn("error stopping auth cleanup", "error", err)
		}
	}
	// And release what auth holds open, which is the geoip database: a file
	// mapped into memory for as long as it is open.
	if r.authManager != nil {
		if err := r.authManager.Close(); err != nil {
			r.logger.Warn("error closing the auth manager", "error", err)
		}
	}

	// Close sync manager
	if r.syncManager != nil {
		if err := r.syncManager.Close(); err != nil {
			r.logger.Warn("error closing sync manager", "error", err)
		}
	}

	// Stop workflow engine and the listener in front of it
	if err := r.stopWorkflowAPI(ctx); err != nil {
		r.logger.Warn("error shutting down workflow api", "error", err)
	}
	if r.workflowEngine != nil {
		r.workflowEngine.Stop()
	}

	// Shutdown admin server if running
	if r.adminServer != nil {
		if err := r.adminServer.Shutdown(ctx); err != nil {
			r.logger.Warn("error shutting down admin server", "error", err)
		}
	}

	// Flush and stop the OpenTelemetry tracer provider (no-op when tracing off).
	if r.traceShutdown != nil {
		if err := r.traceShutdown(ctx); err != nil {
			r.logger.Warn("error shutting down tracing", "error", err)
		}
	}

	// Close all connectors
	if err := r.connectors.CloseAll(ctx); err != nil {
		banner.PrintError("Error closing connectors: " + err.Error())
	}

	return nil
}

// Starter is implemented by connectors that need to start a background process.
type Starter interface {
	Start(ctx context.Context) error
}

// RouteRegistrar is implemented by connectors that can register HTTP routes.
type RouteRegistrar interface {
	RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error))
}

// RouteRegistrarWithReturnType extends RouteRegistrar with return type support.
// Used by GraphQL connectors in HCL-first mode where flow specifies return type.
type RouteRegistrarWithReturnType interface {
	RouteRegistrar
	RegisterRouteWithReturnType(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error), returnType string)
}

// ArgDef defines an argument for a GraphQL field (re-exported from graphql package).
type ArgDef = graphql.ArgDef

// RouteRegistrarWithArgs extends RouteRegistrar with typed arguments support.
// Used by GraphQL connectors to generate proper schema arguments instead of generic JSON input.
type RouteRegistrarWithArgs interface {
	RouteRegistrar
	RegisterRouteWithArgs(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error), returnType string, args []*ArgDef)
}

// SubscriptionPublisher is implemented by connectors that support publishing to subscriptions.
// Used by GraphQL connectors to publish events to subscription topics.
type SubscriptionPublisher interface {
	Publish(topic string, data interface{})
}

// SubscriptionRegistrar is implemented by connectors that can register subscription fields.
// Used by GraphQL connectors to create subscription fields from flow configuration.
type SubscriptionRegistrar interface {
	RegisterSubscription(fieldName string, returnType string)
	RegisterSubscriptionWithFilter(fieldName string, returnType string, filter string)
}

// EntityRegistrar is implemented by connectors that can register federated entities.
// Used by GraphQL connectors to register entity resolvers for Federation.
type EntityRegistrar interface {
	RegisterEntity(typeName string, keys []graphql.EntityKey, resolver graphql.EntityResolver)
	IsFederationEnabled() bool
}

// HCLTypeLoader is implemented by connectors that can load HCL types.
// Used by GraphQL connectors to generate schema from HCL type definitions.
type HCLTypeLoader interface {
	LoadHCLTypes(types map[string]*validate.TypeSchema) error
}

// HealthRegistrar is implemented by connectors that can register health endpoints.
type HealthRegistrar interface {
	SetHealthManager(h *health.Manager)
}

// MetricsRegistrar is implemented by connectors that can register metrics.
type MetricsRegistrar interface {
	SetMetrics(m *metrics.Registry)
}

// RateLimitRegistrar is implemented by connectors that support rate limiting.
type RateLimitRegistrar interface {
	SetRateLimiter(rl *ratelimit.Limiter)
}

// initAspects creates the aspect executor after connectors are ready.
func (r *Runtime) initAspects() error {
	// Skip if no aspects configured
	if r.aspectRegistry.Count() == 0 {
		return nil
	}

	// Create aspect executor with connector registry
	executor, err := aspect.NewExecutor(r.aspectRegistry, r.connectors)
	if err != nil {
		return fmt.Errorf("failed to create aspect executor: %w", err)
	}
	r.aspectExecutor = executor

	// Print aspect info
	fmt.Println()
	fmt.Println("    Aspects:")
	for _, asp := range r.aspectRegistry.All() {
		banner.PrintAspect(asp.Name, string(asp.When), asp.On)
	}

	return nil
}

// initRateLimiter initializes the rate limiter based on service configuration.
func (r *Runtime) initRateLimiter() {
	envDefs := envdefaults.ForEnvironment(r.environment)

	if r.config.ServiceConfig == nil || r.config.ServiceConfig.RateLimit == nil {
		// No explicit rate limit config — use environment default
		if envDefs.RateLimitEnabled {
			r.rateLimiter = ratelimit.New(&ratelimit.Config{
				Enabled:           true,
				RequestsPerSecond: 100,
				Burst:             200,
				ExcludePaths:      []string{"/health", "/health/live", "/health/ready", "/metrics"},
				EnableHeaders:     true,
			})
			r.logger.Info("rate limiting enabled by environment default",
				"environment", r.environment,
				"requests_per_second", 100,
				"burst", 200,
			)
		}
		return
	}

	rlConfig := r.config.ServiceConfig.RateLimit
	if !rlConfig.Enabled {
		r.logger.Info("rate limiting disabled by configuration")
		return
	}

	rlCfg := &ratelimit.Config{
		Enabled:           rlConfig.Enabled,
		RequestsPerSecond: rlConfig.RequestsPerSecond,
		Burst:             rlConfig.Burst,
		KeyExtractor:      rlConfig.KeyExtractor,
		ExcludePaths:      rlConfig.ExcludePaths,
		EnableHeaders:     rlConfig.EnableHeaders,
		Storage:           rlConfig.Storage,
	}

	r.rateLimiter = ratelimit.New(rlCfg)

	r.logger.Info("rate limiting configured",
		"requests_per_second", rlConfig.RequestsPerSecond,
		"burst", rlConfig.Burst,
		"key_extractor", rlConfig.KeyExtractor,
		"exclude_paths", rlConfig.ExcludePaths,
		"storage", rlConfig.Storage,
	)
}

// upgradeRateLimiterToRedis upgrades the rate limiter to use Redis storage if configured.
// Must be called after connectors are initialized.
func (r *Runtime) upgradeRateLimiterToRedis() {
	if r.rateLimiter == nil || r.config.ServiceConfig == nil || r.config.ServiceConfig.RateLimit == nil {
		return
	}
	storage := r.config.ServiceConfig.RateLimit.Storage
	if storage == "" {
		return
	}

	conn, err := r.connectors.Get(storage)
	if err != nil {
		r.logger.Error("rate limit storage connector not found", "connector", storage, "error", err)
		return
	}

	type redisClientProvider interface {
		Client() *goredis.Client
	}

	provider, ok := conn.(redisClientProvider)
	if !ok {
		r.logger.Error("rate limit storage connector does not provide a Redis client", "connector", storage)
		return
	}

	client := provider.Client()
	if client == nil {
		r.logger.Error("rate limit storage connector returned nil Redis client", "connector", storage)
		return
	}

	store := ratelimit.NewRedisStore(client, "mycel:ratelimit")
	r.rateLimiter.SetRedisStore(store)
	r.logger.Info("rate limiting upgraded to distributed Redis storage", "connector", storage)
}

// Helper functions for extracting configuration values

func getString(props map[string]interface{}, key, defaultVal string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// getInt reads an int property for the startup banner.
//
// It delegates so that a string survives: env() returns a string, so
// `port = env("PORT")` used to fall through to the default here and the banner
// announced a port the service was not listening on. The connector factories
// have coerced since 1.19.1; this is the display path catching up.
func getInt(props map[string]interface{}, key string, defaultVal int) int {
	return connector.IntFromProps(props, key, defaultVal)
}

func getBool(props map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := props[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// initHotReload initializes the hot reload system.
func (r *Runtime) initHotReload(ctx context.Context) error {
	r.logger.Info("initializing hot reload",
		"config_dir", r.configDir,
	)

	// Create reloader with hooks
	r.hotReloader = hotreload.NewReloader(&hotreload.ReloaderConfig{
		ConfigPath: r.configDir,
		Logger:     r.logger,
		OnLoad:     r.hotReloadLoad,
		OnValidate: r.hotReloadValidate,
		OnPrepare:  r.hotReloadPrepare,
		OnSwitch:   r.hotReloadSwitch,
		OnRollback: r.hotReloadRollback,
		OnComplete: r.hotReloadComplete,
	})

	// Create file watcher
	var err error
	r.hotWatcher, err = hotreload.NewWatcher(
		&hotreload.Config{
			Enabled:    true,
			Paths:      []string{r.configDir},
			Extensions: []string{".mycel"},
			Debounce:   500 * time.Millisecond,
		},
		r.logger,
		func(ctx context.Context) error {
			return r.hotReloader.Reload(ctx)
		},
		func(ctx context.Context) error {
			return r.hotReloader.Validate(ctx)
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create hot reload watcher: %w", err)
	}

	// Start the watcher
	if err := r.hotWatcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start hot reload watcher: %w", err)
	}

	// Set up SIGHUP handler
	r.signalHandler = hotreload.NewSignalHandler(r.hotWatcher, r.logger)
	r.signalHandler.Start(ctx)

	r.logger.Info("hot reload enabled - configuration changes will be applied automatically")
	r.logger.Info("send SIGHUP to trigger manual reload")

	return nil
}

// Hot reload hooks

func (r *Runtime) hotReloadLoad(ctx context.Context, configPath string) error {
	r.logger.Debug("hot reload: loading new configuration")

	// Parse new configuration
	p := parser.NewHCLParser()
	_, err := p.Parse(ctx, configPath)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	return nil
}

// hotReloadValidate is the dry run the watcher makes before a reload touches
// the running service. It used to do nothing at all — the files were parsed by
// the load hook and that was the whole check — so a configuration that parses
// and is then refused passed the dry run, logged "configuration validation
// passed", and was turned away seconds later by the switch. It now makes
// exactly the checks the switch makes, and nothing it does is visible to the
// running service.
func (r *Runtime) hotReloadValidate(ctx context.Context) error {
	r.logger.Debug("hot reload: validating configuration")

	p := parser.NewHCLParserWithRegistry(r.schemaRegistry)
	newConfig, err := p.Parse(ctx, r.configDir)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	if errs := ValidateFlowSchemas(newConfig, r.schemaRegistry); len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	if errs := ValidateConnectorSchemas(newConfig, r.schemaRegistry); len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}

	return nil
}

func (r *Runtime) hotReloadPrepare(ctx context.Context) error {
	r.logger.Debug("hot reload: preparing new resources")
	// Resources will be prepared during switch
	return nil
}

func (r *Runtime) hotReloadSwitch(ctx context.Context) error {
	r.logger.Info("hot reload: switching to new configuration")

	r.mu.Lock()
	defer r.mu.Unlock()

	// Parse new configuration
	p := parser.NewHCLParserWithRegistry(r.schemaRegistry)
	newConfig, err := p.Parse(ctx, r.configDir)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// The same check startup makes, so a reload cannot install a configuration
	// that would have been refused on the way in.
	if errs := ValidateFlowSchemas(newConfig, r.schemaRegistry); len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	if errs := ValidateConnectorSchemas(newConfig, r.schemaRegistry); len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}

	// Everything below builds the new configuration beside the running one and
	// swaps it in only once it stands up. See reload_state.go: dismantling
	// first left a failed reload serving nothing at all.
	previous := r.snapshotForReload()

	// Create new connector registry
	newRegistry := connector.NewRegistry()
	registerBuiltinFactories(newRegistry, r.logger)

	// Create new operation resolver
	newResolver := connector.NewOperationResolver()

	// Update runtime state
	r.config = newConfig
	r.connectors = newRegistry
	r.operationResolver = newResolver
	r.suspendedStarters = nil

	// Rebuild transforms map
	r.transforms = make(map[string]*transform.Config)
	for _, t := range newConfig.Transforms {
		r.transforms[t.Name] = t
	}

	// Rebuild types map
	r.types = make(map[string]*validate.TypeSchema)
	for _, t := range newConfig.Types {
		r.types[t.Name] = t
	}

	// Rebuild named caches map
	r.namedCaches = make(map[string]*flow.NamedCacheConfig)
	for _, c := range newConfig.NamedCaches {
		r.namedCaches[c.Name] = c
	}

	// Rebuild aspect registry
	r.aspectRegistry = aspect.NewRegistry()
	if err := r.aspectRegistry.RegisterAll(newConfig.Aspects); err != nil {
		r.restore(previous)
		r.abandon(ctx, newRegistry)
		return fmt.Errorf("failed to register aspects: %w", err)
	}

	// Create new flow registry
	r.flows = NewFlowRegistry()

	// Initialize new connectors
	if err := r.initConnectors(ctx); err != nil {
		r.restore(previous)
		r.abandon(ctx, newRegistry)
		return fmt.Errorf("failed to initialize connectors: %w", err)
	}

	// Initialize aspects (creates executor with new connectors)
	if err := r.initAspects(); err != nil {
		r.restore(previous)
		r.abandon(ctx, newRegistry)
		return fmt.Errorf("failed to initialize aspects: %w", err)
	}

	// Register flows with new connectors
	if err := r.registerFlows(); err != nil {
		r.restore(previous)
		r.abandon(ctx, newRegistry)
		return fmt.Errorf("failed to register flows: %w", err)
	}

	// Wire flow invoker into aspect executor
	if r.aspectExecutor != nil {
		r.aspectExecutor.SetFlowInvoker(r.flows)
	}

	// The new configuration stands up, so the old one can go. Until this line
	// the service was still serving it, which is what makes a failed reload
	// harmless rather than fatal.
	if err := previous.connectors.CloseAll(ctx); err != nil {
		r.logger.Warn("some connectors failed to close during reload", "error", err)
	}

	// Note: HTTP/REST/GraphQL/gRPC servers are not restarted here — they're
	// long-lived listeners owned by the runtime, and the new flow handlers
	// were re-registered against them above. That provides zero-downtime
	// reload for request-response connectors.
	//
	// Event-driven connectors (MQ, CDC, file watchers, WebSocket, MQTT) are
	// a different story. CloseAll() above cancelled their consumer
	// goroutines; the freshly-built connectors are connected via
	// initConnectors() but their Start()s have not been called. Without
	// that call, the connector is open but no worker is reading messages —
	// the symptom users see as "after hot reload, RabbitMQ stops
	// consuming". Restart them now.
	debugConnected := r.debugServer != nil && r.debugServer.HasClients()
	debugReady := debugConnected && r.debugServer.IsReady()

	for _, name := range r.connectors.List() {
		conn, err := r.connectors.Get(name)
		if err != nil {
			continue
		}

		// Re-apply debug throttling on the new connector instance regardless
		// of whether we're about to start it now or defer it.
		if debugConnected {
			if throttler, ok := conn.(connector.DebugThrottler); ok {
				throttler.SetDebugMode(true)
			}
		}

		starter, isStarter := conn.(Starter)
		if !isStarter {
			continue
		}

		// If a debugger is connected but hasn't completed the ready
		// handshake, defer Start for event-driven connectors so the IDE
		// can attach controllers before the first message flows.
		if debugConnected && !debugReady {
			if _, isEventDriven := conn.(connector.DebugThrottler); isEventDriven {
				r.suspendedStarters = append(r.suspendedStarters, suspendedConnector{
					name:    name,
					starter: starter,
				})
				r.logger.Info("hot reload: connector start deferred until debugger ready",
					"connector", name)
				continue
			}
		}

		// Health/metrics/rate-limit registration was done by the previous
		// startServers; the new connector instance needs the same wiring
		// so probes and metrics keep working.
		if hr, ok := conn.(HealthRegistrar); ok {
			hr.SetHealthManager(r.health)
		}
		if mr, ok := conn.(MetricsRegistrar); ok {
			mr.SetMetrics(r.metrics)
		}
		if rlr, ok := conn.(RateLimitRegistrar); ok && r.rateLimiter != nil {
			rlr.SetRateLimiter(r.rateLimiter)
		}

		// Re-register flow handlers against the NEW connector instance.
		// CloseAll above wiped the previous instance and its internal
		// handler map; Start() below would otherwise spin up a consumer
		// against an empty registry — symptom: "no handler for routing
		// key" warnings + silent ack-and-drop after every hot reload.
		r.registerFlowHandlers(name, conn)

		r.logger.Info("hot reload: starting connector", "connector", name)
		if err := starter.Start(context.Background()); err != nil {
			r.logger.Error("failed to start connector after hot reload",
				"connector", name, "error", err)
			continue
		}

		// Re-apply debug mode AFTER Start() so it takes effect on the
		// newly created channel/consumer (e.g. RabbitMQ prefetch=1 + gate).
		if debugConnected {
			if throttler, ok := conn.(connector.DebugThrottler); ok {
				throttler.SetDebugMode(true)
			}
		}
	}

	// Drain previously-suspended starters that the debugger already cleared.
	if debugReady && len(r.suspendedStarters) > 0 {
		for _, sc := range r.suspendedStarters {
			r.logger.Info("hot reload: starting suspended connector (debugger ready)",
				"connector", sc.name)
			conn, _ := r.connectors.Get(sc.name)
			if conn != nil {
				// Same handler-map population as the immediate-start path.
				r.registerFlowHandlers(sc.name, conn)
			}
			if err := sc.starter.Start(context.Background()); err != nil {
				r.logger.Error("failed to start suspended connector after hot reload",
					"connector", sc.name, "error", err)
				continue
			}
			if conn != nil {
				if throttler, ok := conn.(connector.DebugThrottler); ok {
					throttler.SetDebugMode(true)
				}
			}
		}
		r.suspendedStarters = nil
	}

	return nil
}

func (r *Runtime) hotReloadRollback(ctx context.Context, err error) {
	r.logger.Warn("hot reload: rolling back due to error", "error", err)
	// The switch function handles rollback internally
}

func (r *Runtime) hotReloadComplete(ctx context.Context) {
	r.logger.Info("hot reload: configuration reload completed successfully")

	// Update metrics
	if r.metrics != nil {
		// Could add a reload counter metric here
	}

	// Mark health as ready
	r.health.SetReady(true)
}

// Reload triggers a manual configuration reload.
func (r *Runtime) Reload(ctx context.Context) error {
	if !r.hotReloadEnabled || r.hotReloader == nil {
		return fmt.Errorf("hot reload is not enabled")
	}
	return r.hotReloader.Reload(ctx)
}

// ReloadStats returns hot reload statistics.
func (r *Runtime) ReloadStats() map[string]interface{} {
	if !r.hotReloadEnabled || r.hotReloader == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	stats := r.hotReloader.Stats()
	stats["enabled"] = true

	if r.hotWatcher != nil {
		stats["watching"] = true
		stats["last_reload"] = r.hotWatcher.LastReload()
		stats["is_reloading"] = r.hotWatcher.IsReloading()
	}

	return stats
}

// AuthManager returns the auth manager, or nil if auth is not configured.
func (r *Runtime) AuthManager() *auth.Manager {
	return r.authManager
}

// AuthHandler returns the auth HTTP handler, or nil if auth is not configured.
func (r *Runtime) AuthHandler() *auth.Handler {
	return r.authHandler
}

// sourceTypeOf reports the connector type a flow is triggered from, or "" for a
// scheduled flow, which is triggered by the clock and names no source.
func (r *Runtime) sourceTypeOf(cfg *flow.Config) string {
	if cfg.From == nil || cfg.From.GetConnector() == "" {
		return ""
	}
	return r.getConnectorType(cfg.From.GetConnector())
}

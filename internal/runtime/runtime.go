// Package runtime provides the core runtime for Mycel services.
// It orchestrates configuration parsing, connector initialization,
// flow registration, and HTTP server lifecycle.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/matutetandil/mycel/internal/aspect"
	"github.com/matutetandil/mycel/internal/auth"
	"github.com/matutetandil/mycel/internal/banner"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/cache"
	"github.com/matutetandil/mycel/internal/connector/discord"
	"github.com/matutetandil/mycel/internal/connector/email"
	"github.com/matutetandil/mycel/internal/connector/profile"
	"github.com/matutetandil/mycel/internal/connector/push"
	"github.com/matutetandil/mycel/internal/connector/slack"
	"github.com/matutetandil/mycel/internal/connector/sms"
	"github.com/matutetandil/mycel/internal/connector/webhook"
	"github.com/matutetandil/mycel/internal/connector/database/mongodb"
	"github.com/matutetandil/mycel/internal/connector/database/mysql"
	"github.com/matutetandil/mycel/internal/connector/database/postgres"
	"github.com/matutetandil/mycel/internal/connector/database/sqlite"
	"github.com/matutetandil/mycel/internal/connector/exec"
	"github.com/matutetandil/mycel/internal/connector/file"
	"github.com/matutetandil/mycel/internal/connector/graphql"
	conns3 "github.com/matutetandil/mycel/internal/connector/s3"
	conngrpc "github.com/matutetandil/mycel/internal/connector/grpc"
	connhttp "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/connector/mq"
	"github.com/matutetandil/mycel/internal/connector/rest"
	"github.com/matutetandil/mycel/internal/connector/tcp"
	connws "github.com/matutetandil/mycel/internal/connector/websocket"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/functions"
	"github.com/matutetandil/mycel/internal/health"
	"github.com/matutetandil/mycel/internal/hotreload"
	"github.com/matutetandil/mycel/internal/mock"
	"github.com/matutetandil/mycel/internal/metrics"
	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/plugin"
	"github.com/matutetandil/mycel/internal/ratelimit"
	"github.com/matutetandil/mycel/internal/scheduler"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
	"github.com/matutetandil/mycel/internal/validate"
)

// Version is the current version of Mycel.
const Version = "0.1.0"

// Runtime orchestrates the lifecycle of a Mycel service.
type Runtime struct {
	config            *parser.Configuration
	connectors        *connector.Registry
	operationResolver *connector.OperationResolver
	flows             *FlowRegistry
	transforms        map[string]*transform.Config
	types             map[string]*validate.TypeSchema
	namedCaches       map[string]*flow.NamedCacheConfig
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

	// Sync manager for distributed locks, semaphores, and coordination
	syncManager *msync.Manager

	// Scheduler for cron-based flow triggers
	scheduler *scheduler.Scheduler

	// Hot reload components
	hotReloadEnabled bool
	hotReloader      *hotreload.Reloader
	hotWatcher       *hotreload.Watcher
	signalHandler    *hotreload.SignalHandler

	// shutdownTimeout is the maximum time to wait for graceful shutdown.
	shutdownTimeout time.Duration

	// mu protects runtime state during hot reload
	mu sync.RWMutex
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

	// MockConnectors is a comma-separated list of connectors to mock.
	// Empty means mock all when mocking is enabled.
	MockConnectors string

	// NoMockConnectors is a comma-separated list of connectors to exclude from mocking.
	NoMockConnectors string
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

	// Parse configuration
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), opts.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configuration: %w", err)
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
	serviceVersion := Version
	if config.ServiceConfig != nil {
		if config.ServiceConfig.Name != "" {
			serviceName = config.ServiceConfig.Name
		}
		if config.ServiceConfig.Version != "" {
			serviceVersion = config.ServiceConfig.Version
		}
	}
	metricsReg := metrics.NewRegistry(serviceName, serviceVersion)
	metrics.SetDefault(metricsReg)

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
	pluginReg := plugin.NewRegistry(opts.ConfigDir)
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

	// Create auth manager if auth config is present
	var authMgr *auth.Manager
	var authHdl *auth.Handler
	if config.Auth != nil {
		var err error
		authMgr, err = auth.NewManager(config.Auth,
			auth.WithLogger(opts.Logger),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create auth manager: %w", err)
		}
		authHdl = auth.NewHandler(authMgr)
		opts.Logger.Info("auth system initialized",
			"preset", config.Auth.Preset)
	}

	// Create scheduler for cron-based flows
	sched := scheduler.New()

	return &Runtime{
		config:            config,
		connectors:        registry,
		operationResolver: opResolver,
		flows:             NewFlowRegistry(),
		transforms:        transforms,
		types:             types,
		namedCaches:       namedCaches,
		health:            healthMgr,
		metrics:           metricsReg,
		aspectRegistry:    aspectReg,
		mockManager:       mockMgr,
		functionsRegistry: functionsReg,
		pluginRegistry:    pluginReg,
		authManager:       authMgr,
		authHandler:       authHdl,
		scheduler:         sched,
		logger:            opts.Logger,
		environment:       env,
		configDir:         opts.ConfigDir,
		hotReloadEnabled:  opts.HotReload,
		shutdownTimeout:   opts.ShutdownTimeout,
	}, nil
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

	// Initialize sync manager (needs connectors to be ready)
	r.syncManager = msync.NewManager(r.connectors)

	// Create aspect executor (needs connectors to be initialized)
	if err := r.initAspects(); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to initialize aspects: %w", err)
	}

	// Register flows
	if err := r.registerFlows(); err != nil {
		banner.PrintError(err.Error())
		return fmt.Errorf("failed to register flows: %w", err)
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

// getRESTPort returns the port of the first REST connector, or 0 if none.
func (r *Runtime) getRESTPort() int {
	for _, cfg := range r.config.Connectors {
		if cfg.Type == "rest" {
			if port, ok := cfg.Properties["port"]; ok {
				if p, ok := port.(int); ok {
					return p
				}
			}
		}
	}
	return 0
}

// initConnectors creates and connects all configured connectors.
func (r *Runtime) initConnectors(ctx context.Context) error {
	fmt.Println("    Connectors:")

	for _, cfg := range r.config.Connectors {
		// Register connector config with operation resolver
		r.operationResolver.Register(cfg)

		if err := r.connectors.Register(ctx, cfg); err != nil {
			return fmt.Errorf("failed to register connector %s: %w", cfg.Name, err)
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
		port := 9000
		if p, ok := cfg.Properties["port"].(int); ok {
			port = p
		}
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
		port := 5672
		if p, ok := cfg.Properties["port"].(int); ok {
			port = p
		}
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
	fmt.Println()
	fmt.Println("    Flows:")

	for _, cfg := range r.config.Flows {
		// Get source connector
		source, err := r.connectors.Get(cfg.From.Connector)
		if err != nil {
			return fmt.Errorf("flow %s: source connector not found: %w", cfg.Name, err)
		}

		// Get destination connector
		dest, err := r.connectors.Get(cfg.To.Connector)
		if err != nil {
			return fmt.Errorf("flow %s: destination connector not found: %w", cfg.Name, err)
		}

		// Register the flow
		handler := &FlowHandler{
			Config:            cfg,
			FlowPath:          cfg.SourceFile,
			Source:            source,
			Dest:              dest,
			NamedTransforms:   r.transforms,
			Types:             r.types,
			Connectors:        r.connectors,
			OperationResolver: r.operationResolver,
			NamedCaches:       r.namedCaches,
			AspectExecutor:    r.aspectExecutor,
			FunctionsRegistry: r.functionsRegistry,
			SyncManager:       r.syncManager,
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
		if isSubscriptionTarget(cfg.To.Operation) {
			if subReg, ok := dest.(SubscriptionRegistrar); ok {
				fieldName := strings.TrimPrefix(cfg.To.Operation, "Subscription.")
				filter := cfg.To.Filter
				if filter != "" {
					subReg.RegisterSubscriptionWithFilter(fieldName, cfg.Returns, filter)
				} else {
					subReg.RegisterSubscription(fieldName, cfg.Returns)
				}
			}
		}

		// Parse operation to get method and path
		method, path := r.parseFlowOperation(cfg.From.Connector, cfg.From.Operation)
		target := cfg.To.Connector + ":" + cfg.To.Target
		if isSubscriptionTarget(cfg.To.Operation) {
			target = cfg.To.Connector + ":" + cfg.To.Operation
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

// startServers starts all REST connector HTTP servers.
func (r *Runtime) startServers(ctx context.Context) error {
	// Find REST connectors and start their servers
	for _, name := range r.connectors.List() {
		conn, _ := r.connectors.Get(name)

		// Check if this is a startable connector (REST)
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

			// Start the server
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start %s: %w", name, err)
			}
		}
	}

	// Mark service as ready after all servers are started
	r.health.SetReady(true)

	return nil
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
		if handler.Config.From.Connector == connectorName {
			// If flow has a return type and connector supports typed args, use RegisterRouteWithArgs
			if hasArgsSupport && handler.Config.Returns != "" {
				args := inferArgsFromFlow(handler.Config)
				routerWithArgs.RegisterRouteWithArgs(
					handler.Config.From.Operation,
					handler.HandleRequest,
					handler.Config.Returns,
					args,
				)
			} else if hasReturnTypeSupport && handler.Config.Returns != "" {
				// Fallback to return type only registration
				routerWithReturnType.RegisterRouteWithReturnType(
					handler.Config.From.Operation,
					handler.HandleRequest,
					handler.Config.Returns,
				)
			} else {
				router.RegisterRoute(handler.Config.From.Operation, handler.HandleRequest)
			}
		}
	}

	// Register federated entity resolvers
	r.registerEntityResolvers(connectorName, conn)
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
			if handler.Config.From.Connector != connectorName {
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
			r.logger.Debug("no entity resolver found for type (register a flow with entity attribute)",
				"type", typeName,
				"connector", connectorName,
			)
		}
	}
}

// inferArgsFromFlow extracts GraphQL arguments from flow step params.
// It looks for expressions like "input.id", "input.name" in step params
// and creates typed argument definitions.
func inferArgsFromFlow(cfg *flow.Config) []*ArgDef {
	args := make(map[string]*ArgDef) // Use map to deduplicate

	// Extract from step params
	for _, step := range cfg.Steps {
		for _, value := range step.Params {
			extractInputArgs(value, args)
		}
	}

	// Convert map to slice
	result := make([]*ArgDef, 0, len(args))
	for _, arg := range args {
		result = append(result, arg)
	}

	return result
}

// extractInputArgs extracts input.* references from a param value.
func extractInputArgs(value interface{}, args map[string]*ArgDef) {
	switch v := value.(type) {
	case string:
		// Look for patterns like "input.id", "input.name", etc.
		// Simple extraction for direct references
		if len(v) > 6 && v[:6] == "input." {
			// Extract the field name (handle "input.id", not "input.nested.field")
			rest := v[6:]
			// Find end of identifier (before any operator or method call)
			endIdx := len(rest)
			for i, ch := range rest {
				if ch == '.' || ch == ' ' || ch == '!' || ch == '=' || ch == '+' || ch == '-' || ch == '*' || ch == '/' || ch == '(' || ch == ')' || ch == '[' || ch == ']' || ch == '?' || ch == ':' {
					endIdx = i
					break
				}
			}
			fieldName := rest[:endIdx]
			if fieldName != "" && args[fieldName] == nil {
				args[fieldName] = &ArgDef{
					Name:        fieldName,
					Type:        "string", // Default to string, could be improved with type inference
					Required:    false,    // Don't make required by default
					Description: fmt.Sprintf("Argument %s (inferred from flow)", fieldName),
				}
			}
		}
	case map[string]interface{}:
		for _, subVal := range v {
			extractInputArgs(subVal, args)
		}
	}
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
func (r *Runtime) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
	defer cancel()

	banner.PrintShutdown()

	// Mark service as not ready (stop accepting new traffic)
	r.health.SetReady(false)

	// Stop the scheduler
	if r.scheduler != nil {
		<-r.scheduler.Stop().Done()
	}

	// Close sync manager
	if r.syncManager != nil {
		if err := r.syncManager.Close(); err != nil {
			r.logger.Warn("error closing sync manager", "error", err)
		}
	}

	// Close all connectors
	if err := r.connectors.CloseAll(ctx); err != nil {
		banner.PrintError("Error closing connectors: " + err.Error())
	}

	banner.PrintGoodbye()
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
	if r.config.ServiceConfig == nil || r.config.ServiceConfig.RateLimit == nil {
		return
	}

	rlConfig := r.config.ServiceConfig.RateLimit
	if !rlConfig.Enabled {
		r.logger.Info("rate limiting disabled by configuration")
		return
	}

	r.rateLimiter = ratelimit.New(&ratelimit.Config{
		Enabled:           rlConfig.Enabled,
		RequestsPerSecond: rlConfig.RequestsPerSecond,
		Burst:             rlConfig.Burst,
		KeyExtractor:      rlConfig.KeyExtractor,
		ExcludePaths:      rlConfig.ExcludePaths,
		EnableHeaders:     rlConfig.EnableHeaders,
	})

	r.logger.Info("rate limiting configured",
		"requests_per_second", rlConfig.RequestsPerSecond,
		"burst", rlConfig.Burst,
		"key_extractor", rlConfig.KeyExtractor,
		"exclude_paths", rlConfig.ExcludePaths,
	)
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

func getInt(props map[string]interface{}, key string, defaultVal int) int {
	if v, ok := props[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return defaultVal
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
			Extensions: []string{".hcl"},
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

func (r *Runtime) hotReloadValidate(ctx context.Context) error {
	r.logger.Debug("hot reload: validating configuration")
	// Configuration validation happens during load
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
	p := parser.NewHCLParser()
	newConfig, err := p.Parse(ctx, r.configDir)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Close existing connectors gracefully
	if err := r.connectors.CloseAll(ctx); err != nil {
		r.logger.Warn("some connectors failed to close during reload", "error", err)
	}

	// Create new connector registry
	newRegistry := connector.NewRegistry()
	registerBuiltinFactories(newRegistry, r.logger)

	// Create new operation resolver
	newResolver := connector.NewOperationResolver()

	// Update runtime state
	oldConfig := r.config
	r.config = newConfig
	r.connectors = newRegistry
	r.operationResolver = newResolver

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
		r.config = oldConfig
		return fmt.Errorf("failed to register aspects: %w", err)
	}

	// Create new flow registry
	r.flows = NewFlowRegistry()

	// Initialize new connectors
	if err := r.initConnectors(ctx); err != nil {
		// Rollback to old config
		r.config = oldConfig
		return fmt.Errorf("failed to initialize connectors: %w", err)
	}

	// Initialize aspects (creates executor with new connectors)
	if err := r.initAspects(); err != nil {
		r.config = oldConfig
		return fmt.Errorf("failed to initialize aspects: %w", err)
	}

	// Register flows with new connectors
	if err := r.registerFlows(); err != nil {
		r.config = oldConfig
		return fmt.Errorf("failed to register flows: %w", err)
	}

	// Note: We don't restart HTTP servers here because they're already running
	// and the new flows are registered with them. This provides zero-downtime reload.

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

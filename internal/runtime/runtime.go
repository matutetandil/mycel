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
	"syscall"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/connector/database/sqlite"
	"github.com/mycel-labs/mycel/internal/connector/rest"
	"github.com/mycel-labs/mycel/internal/parser"
)

// Runtime orchestrates the lifecycle of a Mycel service.
type Runtime struct {
	config     *parser.Configuration
	connectors *connector.Registry
	flows      *FlowRegistry
	logger     *slog.Logger

	// shutdownTimeout is the maximum time to wait for graceful shutdown.
	shutdownTimeout time.Duration
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

	// Register built-in connector factories
	registerBuiltinFactories(registry, opts.Logger)

	return &Runtime{
		config:          config,
		connectors:      registry,
		flows:           NewFlowRegistry(),
		logger:          opts.Logger,
		shutdownTimeout: opts.ShutdownTimeout,
	}, nil
}

// registerBuiltinFactories registers the built-in connector factories.
func registerBuiltinFactories(registry *connector.Registry, logger *slog.Logger) {
	// REST connector for exposing HTTP endpoints
	registry.RegisterFactory(rest.NewFactory(logger))

	// SQLite database connector
	registry.RegisterFactory(sqlite.NewFactory(logger))

	// Future connectors:
	// - PostgreSQL connector factory
	// - MySQL connector factory
	// - Redis connector factory
	// - Kafka connector factory
	// - etc.
}

// Start initializes all connectors, registers flows, and starts the HTTP server.
// It blocks until a shutdown signal is received or the context is cancelled.
func (r *Runtime) Start(ctx context.Context) error {
	r.printBanner()

	// Initialize connectors
	if err := r.initConnectors(ctx); err != nil {
		return fmt.Errorf("failed to initialize connectors: %w", err)
	}

	// Register flows
	if err := r.registerFlows(); err != nil {
		return fmt.Errorf("failed to register flows: %w", err)
	}

	// Start REST connectors (HTTP servers)
	if err := r.startServers(ctx); err != nil {
		return fmt.Errorf("failed to start servers: %w", err)
	}

	r.logger.Info("Ready! Press Ctrl+C to stop.")

	// Wait for shutdown signal
	return r.waitForShutdown(ctx)
}

// printBanner prints the startup banner.
func (r *Runtime) printBanner() {
	serviceName := "mycel-service"
	serviceVersion := "0.1.0"
	if r.config.ServiceConfig != nil {
		if r.config.ServiceConfig.Name != "" {
			serviceName = r.config.ServiceConfig.Name
		}
		if r.config.ServiceConfig.Version != "" {
			serviceVersion = r.config.ServiceConfig.Version
		}
	}

	fmt.Println()
	fmt.Printf("  ╭─────────────────────────────────────╮\n")
	fmt.Printf("  │  %s v%s\n", serviceName, serviceVersion)
	fmt.Printf("  ╰─────────────────────────────────────╯\n")
	fmt.Println()
}

// initConnectors creates and connects all configured connectors.
func (r *Runtime) initConnectors(ctx context.Context) error {
	r.logger.Info("Initializing connectors...")

	for _, cfg := range r.config.Connectors {
		if err := r.connectors.Register(ctx, cfg); err != nil {
			return fmt.Errorf("failed to register connector %s: %w", cfg.Name, err)
		}

		r.logger.Info("Connector initialized",
			slog.String("name", cfg.Name),
			slog.String("type", cfg.Type),
		)
	}

	// Connect all connectors
	if err := r.connectors.ConnectAll(ctx); err != nil {
		return err
	}

	return nil
}

// registerFlows builds flow handlers from configuration.
func (r *Runtime) registerFlows() error {
	r.logger.Info("Registering flows...")

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
			Config: cfg,
			Source: source,
			Dest:   dest,
		}

		r.flows.Register(cfg.Name, handler)

		r.logger.Info("Flow registered",
			slog.String("name", cfg.Name),
			slog.String("operation", cfg.From.Operation),
			slog.String("target", cfg.To.Target),
		)
	}

	return nil
}

// startServers starts all REST connector HTTP servers.
func (r *Runtime) startServers(ctx context.Context) error {
	r.logger.Info("Starting servers...")

	// Find REST connectors and start their servers
	for _, name := range r.connectors.List() {
		conn, _ := r.connectors.Get(name)

		// Check if this is a startable connector (REST)
		if starter, ok := conn.(Starter); ok {
			// Register flow handlers for this connector
			r.registerFlowHandlers(name, conn)

			// Start the server
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start %s: %w", name, err)
			}
		}
	}

	return nil
}

// registerFlowHandlers registers HTTP handlers for flows using this connector.
func (r *Runtime) registerFlowHandlers(connectorName string, conn connector.Connector) {
	router, ok := conn.(RouteRegistrar)
	if !ok {
		r.logger.Debug("Connector does not support route registration", slog.String("connector", connectorName))
		return
	}

	// Find flows that use this connector as source
	for name, handler := range r.flows.handlers {
		if handler.Config.From.Connector == connectorName {
			r.logger.Info("Registering HTTP route",
				slog.String("flow", name),
				slog.String("operation", handler.Config.From.Operation),
			)
			router.RegisterRoute(handler.Config.From.Operation, handler.HandleRequest)
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
	case sig := <-sigChan:
		r.logger.Info("Received signal, shutting down...", slog.String("signal", sig.String()))
	case <-ctx.Done():
		r.logger.Info("Context cancelled, shutting down...")
	}

	return r.Shutdown()
}

// Shutdown gracefully shuts down the runtime.
func (r *Runtime) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
	defer cancel()

	r.logger.Info("Shutting down gracefully...")

	// Close all connectors
	if err := r.connectors.CloseAll(ctx); err != nil {
		r.logger.Error("Error closing connectors", slog.Any("error", err))
	}

	r.logger.Info("Goodbye!")
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

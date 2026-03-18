// Package cdc provides a Change Data Capture connector that streams
// database changes (INSERT/UPDATE/DELETE) as flow events in real-time.
// Currently supports PostgreSQL via logical replication (pgoutput).
package cdc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// HandlerFunc is a function that handles a CDC event.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Config holds CDC connector configuration.
type Config struct {
	Driver      string
	Host        string
	Port        int
	Database    string
	User        string
	Password    string
	SSLMode     string
	SlotName    string
	Publication string
}

// Event represents a single database change event.
type Event struct {
	Trigger   string                 // INSERT, UPDATE, DELETE
	Schema    string                 // e.g., "public"
	Table     string                 // e.g., "users"
	New       map[string]interface{} // New row data (INSERT/UPDATE)
	Old       map[string]interface{} // Old row data (UPDATE/DELETE)
	Timestamp time.Time
}

// Listener is the interface for driver-specific CDC implementations.
type Listener interface {
	Start(ctx context.Context, eventCh chan<- *Event) error
	Close() error
}

// Connector implements a CDC connector that streams database changes as flow events.
type Connector struct {
	name     string
	config   *Config
	logger   *slog.Logger
	listener Listener

	mu       sync.RWMutex
	handlers map[string]HandlerFunc // "INSERT:users" → handler
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc

	// Debug throttling: single-message processing when debugger is connected
	debugGate connector.DebugGate
}

// New creates a new CDC connector.
func New(name string, config *Config, listener Listener, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Connector{
		name:     name,
		config:   config,
		logger:   logger,
		listener: listener,
		handlers: make(map[string]HandlerFunc),
	}
}

// Name returns the connector identifier.
func (c *Connector) Name() string { return c.name }

// Type returns "cdc".
func (c *Connector) Type() string { return "cdc" }

// Connect is a no-op; the listener connects on Start.
func (c *Connector) Connect(ctx context.Context) error { return nil }

// Close stops the CDC listener and cancels the dispatch loop.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	if c.listener != nil {
		return c.listener.Close()
	}
	c.started = false
	return nil
}

// Health checks if the connector is running.
func (c *Connector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.started {
		return fmt.Errorf("cdc connector %s not started", c.name)
	}
	return nil
}

// RegisterRoute registers a handler for a CDC operation (e.g., "INSERT:users").
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := normalizeOperation(operation)
	if existing, ok := c.handlers[op]; ok {
		c.handlers[op] = HandlerFunc(connector.ChainEventDriven(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "operation", op)
	} else {
		c.handlers[op] = handler
	}
}

// normalizeOperation normalizes an operation string: uppercase trigger, lowercase table.
func normalizeOperation(op string) string {
	trigger, table := ParseOperation(op)
	return trigger + ":" + table
}

// SetDebugMode enables or disables single-message debug throttling.
func (c *Connector) SetDebugMode(enabled bool) {
	c.debugGate.SetEnabled(enabled)
	if enabled {
		c.logger.Info("debug mode enabled: single-event processing", "connector", c.name)
	} else {
		c.logger.Info("debug mode disabled: concurrent processing restored", "connector", c.name)
	}
}

// Start begins listening for CDC events and dispatching them to handlers.
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.started = true
	c.mu.Unlock()

	eventCh := make(chan *Event, 256)

	// Start the listener in a goroutine
	go func() {
		if err := c.listener.Start(c.ctx, eventCh); err != nil {
			if c.ctx.Err() == nil {
				c.logger.Error("CDC listener error", "connector", c.name, "error", err)
			}
		}
	}()

	c.logger.Info("CDC connector started",
		"connector", c.name,
		"driver", c.config.Driver,
		"slot", c.config.SlotName,
		"publication", c.config.Publication,
	)

	// Start dispatch loop in a goroutine
	go c.dispatchLoop(eventCh)

	return nil
}

// dispatchLoop reads events from the channel and dispatches them to matching handlers.
func (c *Connector) dispatchLoop(eventCh <-chan *Event) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			c.dispatchEvent(event)
		}
	}
}

// dispatchEvent finds and invokes the matching handler for an event.
func (c *Connector) dispatchEvent(event *Event) {
	c.mu.RLock()
	handlers := make(map[string]HandlerFunc, len(c.handlers))
	for k, v := range c.handlers {
		handlers[k] = v
	}
	c.mu.RUnlock()

	input := eventToInput(event)
	trigger := strings.ToUpper(event.Trigger)
	table := strings.ToLower(event.Table)

	// Try matches in order of specificity
	keys := []string{
		trigger + ":" + table,   // exact: INSERT:users
		"*:" + table,            // wildcard trigger: *:users
		trigger + ":*",          // wildcard table: INSERT:*
		"*:*",                   // global wildcard
		"*",                     // shorthand global wildcard
	}

	for _, key := range keys {
		if handler, ok := handlers[key]; ok {
			c.debugGate.Acquire()
			_, err := handler(c.ctx, input)
			c.debugGate.Release()
			if err != nil {
				c.logger.Error("CDC handler error",
					"connector", c.name,
					"operation", trigger+":"+table,
					"error", err,
				)
			}
			return
		}
	}

	c.logger.Debug("No handler for CDC event",
		"connector", c.name,
		"trigger", trigger,
		"table", table,
	)
}

// eventToInput converts a CDC Event to the input map passed to flow handlers.
func eventToInput(event *Event) map[string]interface{} {
	input := map[string]interface{}{
		"trigger":   event.Trigger,
		"table":     event.Table,
		"schema":    event.Schema,
		"timestamp": event.Timestamp.Format(time.RFC3339),
	}
	if event.New != nil {
		input["new"] = event.New
	}
	if event.Old != nil {
		input["old"] = event.Old
	}
	return input
}

// ParseOperation splits an operation string like "INSERT:users" into trigger and table.
func ParseOperation(operation string) (trigger, table string) {
	parts := strings.SplitN(operation, ":", 2)
	if len(parts) == 2 {
		return strings.ToUpper(parts[0]), strings.ToLower(parts[1])
	}
	return strings.ToUpper(operation), "*"
}

package trace

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// Collector receives and stores trace events.
type Collector interface {
	// Record adds a trace event.
	Record(event Event)

	// Events returns all collected events.
	Events() []Event
}

// MemoryCollector stores trace events in memory for later rendering.
// Used by the `mycel trace` CLI command.
type MemoryCollector struct {
	mu     sync.Mutex
	events []Event
}

// NewMemoryCollector creates a new in-memory collector.
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{
		events: make([]Event, 0, 16),
	}
}

// Record adds an event to the in-memory store.
func (c *MemoryCollector) Record(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

// Events returns all collected events.
func (c *MemoryCollector) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, len(c.events))
	copy(result, c.events)
	return result
}

// LogCollector writes trace events to a structured logger in real-time.
// Used by the `--verbose-flow` flag during `mycel start`.
type LogCollector struct {
	logger *slog.Logger
	mu     sync.Mutex
	events []Event
}

// NewLogCollector creates a collector that logs events as they occur.
func NewLogCollector(logger *slog.Logger) *LogCollector {
	return &LogCollector{
		logger: logger,
		events: make([]Event, 0, 16),
	}
}

// Record logs the event and stores it.
func (c *LogCollector) Record(event Event) {
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()

	attrs := []slog.Attr{
		slog.String("stage", string(event.Stage)),
	}
	if event.Name != "" {
		attrs = append(attrs, slog.String("name", event.Name))
	}
	if event.Duration > 0 {
		attrs = append(attrs, slog.Duration("duration", event.Duration))
	}
	if event.Skipped {
		attrs = append(attrs, slog.Bool("skipped", true))
	}
	if event.DryRun {
		attrs = append(attrs, slog.Bool("dry_run", true))
	}
	if event.Detail != "" {
		attrs = append(attrs, slog.String("detail", event.Detail))
	}
	if event.Output != nil {
		attrs = append(attrs, slog.String("data", truncateJSON(event.Output, 200)))
	}

	if event.Error != nil {
		attrs = append(attrs, slog.String("error", event.Error.Error()))
		c.logger.LogAttrs(nil, slog.LevelWarn, "trace", attrs...)
	} else {
		c.logger.LogAttrs(nil, slog.LevelDebug, "trace", attrs...)
	}
}

// Events returns all collected events.
func (c *LogCollector) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, len(c.events))
	copy(result, c.events)
	return result
}

// truncateJSON serializes data to JSON and truncates to maxLen characters.
func truncateJSON(data interface{}, maxLen int) string {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	s := string(b)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

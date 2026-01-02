package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// TriggerType represents the type of flow trigger.
type TriggerType int

const (
	// TriggerAlways means the flow is triggered by its "from" connector.
	TriggerAlways TriggerType = iota
	// TriggerCron means the flow is triggered by a cron schedule.
	TriggerCron
	// TriggerInterval means the flow is triggered at regular intervals.
	TriggerInterval
)

// Scheduler manages scheduled flow executions.
type Scheduler struct {
	cron    *cron.Cron
	entries map[string]cron.EntryID
	mu      sync.RWMutex
	running bool
}

// ScheduleConfig contains configuration for a scheduled flow.
type ScheduleConfig struct {
	FlowName string
	When     string // "always", cron expression, or "@every X"
	Handler  func(ctx context.Context) error
}

// New creates a new scheduler.
func New() *Scheduler {
	return &Scheduler{
		cron: cron.New(cron.WithParser(cron.NewParser(
			cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		))),
		entries: make(map[string]cron.EntryID),
	}
}

// ParseWhen parses a "when" value and returns its trigger type.
func ParseWhen(when string) (TriggerType, error) {
	when = strings.TrimSpace(when)

	if when == "" || when == "always" {
		return TriggerAlways, nil
	}

	if strings.HasPrefix(when, "@every ") {
		// Validate interval format
		interval := strings.TrimPrefix(when, "@every ")
		if _, err := time.ParseDuration(interval); err != nil {
			return TriggerAlways, fmt.Errorf("invalid interval %q: %w", interval, err)
		}
		return TriggerInterval, nil
	}

	// Check for shortcuts
	if isShortcut(when) {
		return TriggerCron, nil
	}

	// Must be a cron expression - validate it
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(when); err != nil {
		return TriggerAlways, fmt.Errorf("invalid cron expression %q: %w", when, err)
	}

	return TriggerCron, nil
}

// isShortcut checks if the value is a predefined cron shortcut.
func isShortcut(when string) bool {
	shortcuts := []string{
		"@yearly", "@annually",
		"@monthly",
		"@weekly",
		"@daily", "@midnight",
		"@hourly",
	}
	for _, s := range shortcuts {
		if when == s {
			return true
		}
	}
	return false
}

// Schedule adds a flow to the schedule.
func (s *Scheduler) Schedule(cfg *ScheduleConfig) error {
	triggerType, err := ParseWhen(cfg.When)
	if err != nil {
		return err
	}

	if triggerType == TriggerAlways {
		// No scheduling needed for always triggers
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if any
	if entryID, exists := s.entries[cfg.FlowName]; exists {
		s.cron.Remove(entryID)
		delete(s.entries, cfg.FlowName)
	}

	// Create wrapper function that handles context
	handler := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := cfg.Handler(ctx); err != nil {
			// Log error (could be improved with proper logging)
			fmt.Printf("scheduled flow %q execution error: %v\n", cfg.FlowName, err)
		}
	}

	var entryID cron.EntryID
	if triggerType == TriggerInterval {
		// Parse @every format
		interval := strings.TrimPrefix(cfg.When, "@every ")
		entryID, err = s.cron.AddFunc("@every "+interval, handler)
	} else {
		// Cron expression or shortcut
		entryID, err = s.cron.AddFunc(cfg.When, handler)
	}

	if err != nil {
		return fmt.Errorf("failed to schedule flow %q: %w", cfg.FlowName, err)
	}

	s.entries[cfg.FlowName] = entryID
	return nil
}

// Unschedule removes a flow from the schedule.
func (s *Scheduler) Unschedule(flowName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.entries[flowName]; exists {
		s.cron.Remove(entryID)
		delete(s.entries, flowName)
	}
}

// Start begins executing scheduled flows.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		s.cron.Start()
		s.running = true
	}
}

// Stop stops the scheduler gracefully.
func (s *Scheduler) Stop() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false
	return s.cron.Stop()
}

// IsRunning returns whether the scheduler is running.
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Entries returns all scheduled entries.
func (s *Scheduler) Entries() []ScheduleEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries []ScheduleEntry
	for flowName, entryID := range s.entries {
		entry := s.cron.Entry(entryID)
		entries = append(entries, ScheduleEntry{
			FlowName: flowName,
			Next:     entry.Next,
			Prev:     entry.Prev,
		})
	}
	return entries
}

// ScheduleEntry represents information about a scheduled flow.
type ScheduleEntry struct {
	FlowName string
	Next     time.Time
	Prev     time.Time
}

// GetNextRun returns the next scheduled run time for a flow.
func (s *Scheduler) GetNextRun(flowName string) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if entryID, exists := s.entries[flowName]; exists {
		entry := s.cron.Entry(entryID)
		return entry.Next, true
	}
	return time.Time{}, false
}

// Stats returns scheduler statistics.
func (s *Scheduler) Stats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"scheduled_flows": len(s.entries),
		"running":         s.running,
	}
}

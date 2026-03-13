package aspect

import (
	"path/filepath"
	"sort"
	"sync"
)

// Registry stores and manages aspects.
type Registry struct {
	mu      sync.RWMutex
	aspects []*Config

	// Cache of flow name -> matching aspects
	matchCache map[string][]*Config
}

// NewRegistry creates a new aspect registry.
func NewRegistry() *Registry {
	return &Registry{
		aspects:    make([]*Config, 0),
		matchCache: make(map[string][]*Config),
	}
}

// Register adds an aspect to the registry.
func (r *Registry) Register(aspect *Config) error {
	if err := aspect.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.aspects = append(r.aspects, aspect)

	// Clear match cache when aspects change
	r.matchCache = make(map[string][]*Config)

	return nil
}

// RegisterAll adds multiple aspects to the registry.
func (r *Registry) RegisterAll(aspects []*Config) error {
	for _, aspect := range aspects {
		if err := r.Register(aspect); err != nil {
			return err
		}
	}
	return nil
}

// Match returns all aspects that match the given flow name.
// Results are sorted by priority and when (before < around < after).
func (r *Registry) Match(flowName string) []*Config {
	r.mu.RLock()

	// Check cache first
	if cached, ok := r.matchCache[flowName]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	// Find matching aspects
	var matches []*Config
	r.mu.RLock()
	for _, aspect := range r.aspects {
		if r.matchesAny(flowName, aspect.On) {
			matches = append(matches, aspect)
		}
	}
	r.mu.RUnlock()

	// Sort by priority, then by when order
	sort.SliceStable(matches, func(i, j int) bool {
		// First by priority (lower = first)
		if matches[i].Priority != matches[j].Priority {
			return matches[i].Priority < matches[j].Priority
		}
		// Then by when order: before < around < after
		return whenOrder(matches[i].When) < whenOrder(matches[j].When)
	})

	// Cache the result
	r.mu.Lock()
	r.matchCache[flowName] = matches
	r.mu.Unlock()

	return matches
}

// MatchByWhen returns aspects matching the flow name filtered by when.
func (r *Registry) MatchByWhen(flowName string, when When) []*Config {
	all := r.Match(flowName)
	var filtered []*Config
	for _, aspect := range all {
		if aspect.When == when {
			filtered = append(filtered, aspect)
		}
	}
	return filtered
}

// GetBefore returns all "before" aspects for a flow.
func (r *Registry) GetBefore(flowName string) []*Config {
	return r.MatchByWhen(flowName, Before)
}

// GetAfter returns all "after" aspects for a flow.
func (r *Registry) GetAfter(flowName string) []*Config {
	return r.MatchByWhen(flowName, After)
}

// GetAround returns all "around" aspects for a flow.
func (r *Registry) GetAround(flowName string) []*Config {
	return r.MatchByWhen(flowName, Around)
}

// GetOnError returns all "on_error" aspects for a flow.
func (r *Registry) GetOnError(flowName string) []*Config {
	return r.MatchByWhen(flowName, OnError)
}

// All returns all registered aspects.
func (r *Registry) All() []*Config {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Config, len(r.aspects))
	copy(result, r.aspects)
	return result
}

// Clear removes all aspects from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.aspects = make([]*Config, 0)
	r.matchCache = make(map[string][]*Config)
}

// Count returns the number of registered aspects.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.aspects)
}

// matchesAny checks if the flow name matches any of the patterns.
// Patterns are matched against the flow name using filepath.Match glob syntax.
// Examples: "create_*" matches "create_user", "*" matches everything.
func (r *Registry) matchesAny(flowName string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, flowName); matched {
			return true
		}
	}
	return false
}

// whenOrder returns the order value for a When type.
func whenOrder(w When) int {
	switch w {
	case Before:
		return 0
	case Around:
		return 1
	case After:
		return 2
	case OnError:
		return 3
	default:
		return 99
	}
}

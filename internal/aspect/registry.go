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

	// Cache of flow -> matching aspects
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

// Match returns all aspects that match the given flow path.
// Results are sorted by priority and when (before < around < after).
func (r *Registry) Match(flowPath string) []*Config {
	r.mu.RLock()

	// Check cache first
	if cached, ok := r.matchCache[flowPath]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	// Find matching aspects
	var matches []*Config
	r.mu.RLock()
	for _, aspect := range r.aspects {
		if r.matchesAny(flowPath, aspect.On) {
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
	r.matchCache[flowPath] = matches
	r.mu.Unlock()

	return matches
}

// MatchByWhen returns aspects matching the flow path filtered by when.
func (r *Registry) MatchByWhen(flowPath string, when When) []*Config {
	all := r.Match(flowPath)
	var filtered []*Config
	for _, aspect := range all {
		if aspect.When == when {
			filtered = append(filtered, aspect)
		}
	}
	return filtered
}

// GetBefore returns all "before" aspects for a flow.
func (r *Registry) GetBefore(flowPath string) []*Config {
	return r.MatchByWhen(flowPath, Before)
}

// GetAfter returns all "after" aspects for a flow.
func (r *Registry) GetAfter(flowPath string) []*Config {
	return r.MatchByWhen(flowPath, After)
}

// GetAround returns all "around" aspects for a flow.
func (r *Registry) GetAround(flowPath string) []*Config {
	return r.MatchByWhen(flowPath, Around)
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

// matchesAny checks if the flow path matches any of the patterns.
func (r *Registry) matchesAny(flowPath string, patterns []string) bool {
	// Normalize the flow path
	flowPath = filepath.ToSlash(flowPath)

	for _, pattern := range patterns {
		pattern = filepath.ToSlash(pattern)

		// Use filepath.Match for simple patterns
		if matched, _ := filepath.Match(pattern, flowPath); matched {
			return true
		}

		// Handle ** (double star) for recursive matching
		if matchDoublestar(pattern, flowPath) {
			return true
		}
	}
	return false
}

// matchDoublestar handles ** patterns for recursive directory matching.
func matchDoublestar(pattern, path string) bool {
	// Split pattern by **
	parts := splitByDoublestar(pattern)
	if len(parts) == 1 {
		// No ** in pattern, use simple match
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// For patterns like "**/foo.hcl" (starts with **), match the end
	if len(parts) == 2 && parts[0] == "" {
		suffixPattern := parts[1]
		// Match against each path segment from the end
		pathParts := splitPath(path)
		for i := len(pathParts) - 1; i >= 0; i-- {
			// Try matching the suffix pattern against path segments
			candidate := filepath.ToSlash(filepath.Join(pathParts[i:]...))
			if matched, _ := filepath.Match(suffixPattern, candidate); matched {
				return true
			}
			// Also try matching just the filename
			if i == len(pathParts)-1 {
				if matched, _ := filepath.Match(suffixPattern, pathParts[i]); matched {
					return true
				}
			}
		}
		return false
	}

	// For patterns like "foo/**/bar.hcl" or "foo/**"
	remaining := path
	for i, part := range parts {
		if part == "" {
			continue
		}

		if i == 0 {
			// First part must match at the beginning
			if !hasPrefix(remaining, part) {
				return false
			}
			remaining = remaining[len(matchPrefix(remaining, part)):]
		} else if i == len(parts)-1 {
			// Last part must match at the end
			// For the last part, match against path segments
			pathParts := splitPath(remaining)
			matched := false
			for j := len(pathParts) - 1; j >= 0; j-- {
				candidate := filepath.ToSlash(filepath.Join(pathParts[j:]...))
				if m, _ := filepath.Match(part, candidate); m {
					matched = true
					break
				}
				if j == len(pathParts)-1 {
					if m, _ := filepath.Match(part, pathParts[j]); m {
						matched = true
						break
					}
				}
			}
			if !matched {
				return false
			}
		} else {
			// Middle parts must match somewhere
			idx := findMatch(remaining, part)
			if idx < 0 {
				return false
			}
			remaining = remaining[idx+len(part):]
		}
	}

	return true
}

// splitByDoublestar splits a pattern by ** separator.
func splitByDoublestar(pattern string) []string {
	var parts []string
	current := ""
	i := 0
	for i < len(pattern) {
		if i+1 < len(pattern) && pattern[i] == '*' && pattern[i+1] == '*' {
			parts = append(parts, current)
			current = ""
			i += 2
			// Skip trailing slash after **
			if i < len(pattern) && pattern[i] == '/' {
				i++
			}
		} else {
			current += string(pattern[i])
			i++
		}
	}
	if current != "" || len(parts) > 0 {
		parts = append(parts, current)
	}
	return parts
}

// hasPrefix checks if path starts with pattern (with glob support).
func hasPrefix(path, pattern string) bool {
	if len(pattern) > len(path) {
		return false
	}
	matched, _ := filepath.Match(pattern, path[:len(pattern)])
	return matched
}

// matchPrefix returns the matching prefix.
func matchPrefix(path, pattern string) string {
	for i := len(pattern); i <= len(path); i++ {
		if matched, _ := filepath.Match(pattern, path[:i]); matched {
			return path[:i]
		}
	}
	return ""
}

// hasSuffix checks if path ends with pattern (with glob support).
func hasSuffix(path, pattern string) bool {
	if len(pattern) > len(path) {
		return false
	}
	matched, _ := filepath.Match(pattern, path[len(path)-len(pattern):])
	return matched
}

// findMatch finds the first occurrence of pattern in path.
func findMatch(path, pattern string) int {
	for i := 0; i <= len(path)-len(pattern); i++ {
		if matched, _ := filepath.Match(pattern, path[i:i+len(pattern)]); matched {
			return i
		}
	}
	return -1
}

// splitPath splits a path into segments by forward slash.
func splitPath(path string) []string {
	// Normalize to forward slashes
	path = filepath.ToSlash(path)

	var parts []string
	current := ""

	for _, c := range path {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
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
	default:
		return 99
	}
}

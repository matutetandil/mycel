package mock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Loader loads and caches mock files.
type Loader struct {
	basePath string
	mu       sync.RWMutex
	cache    map[string]*MockFile
}

// NewLoader creates a new mock file loader.
func NewLoader(basePath string) *Loader {
	return &Loader{
		basePath: basePath,
		cache:    make(map[string]*MockFile),
	}
}

// LoadConnectorMock loads a mock file for a connector/target combination.
// Path format: mocks/connectors/{connector}/{target}.json
func (l *Loader) LoadConnectorMock(connector, target string) (*MockFile, error) {
	// Normalize target name for file lookup
	// e.g., "users" -> "users.json"
	// e.g., "GET /users" -> "GET_users.json"
	filename := normalizeTarget(target) + ".json"
	path := filepath.Join(l.basePath, "connectors", connector, filename)

	return l.loadFile(path)
}

// LoadFlowMock loads a mock file for an entire flow.
// Path format: mocks/flows/{flow_name}.json
func (l *Loader) LoadFlowMock(flowName string) (*MockFile, error) {
	filename := flowName + ".json"
	path := filepath.Join(l.basePath, "flows", filename)

	return l.loadFile(path)
}

// LoadOperationMock loads a mock for a specific operation.
// Path format: mocks/connectors/{connector}/{METHOD}_{path}.json
// e.g., mocks/connectors/external_api/GET_users.json
func (l *Loader) LoadOperationMock(connector, method, urlPath string) (*MockFile, error) {
	// Convert path to filename: /users/:id -> users_id
	pathPart := strings.ReplaceAll(urlPath, "/", "_")
	pathPart = strings.ReplaceAll(pathPart, ":", "")
	pathPart = strings.TrimPrefix(pathPart, "_")

	filename := fmt.Sprintf("%s_%s.json", strings.ToUpper(method), pathPart)
	path := filepath.Join(l.basePath, "connectors", connector, filename)

	return l.loadFile(path)
}

// loadFile loads and parses a mock file, with caching.
func (l *Loader) loadFile(path string) (*MockFile, error) {
	// Check cache first
	l.mu.RLock()
	if mock, ok := l.cache[path]; ok {
		l.mu.RUnlock()
		return mock, nil
	}
	l.mu.RUnlock()

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil // No mock file, not an error
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading mock file %s: %w", path, err)
	}

	// Parse JSON
	var mock MockFile
	if err := json.Unmarshal(data, &mock); err != nil {
		return nil, fmt.Errorf("parsing mock file %s: %w", path, err)
	}

	// Cache it
	l.mu.Lock()
	l.cache[path] = &mock
	l.mu.Unlock()

	return &mock, nil
}

// ClearCache clears the mock file cache (useful for hot reload).
func (l *Loader) ClearCache() {
	l.mu.Lock()
	l.cache = make(map[string]*MockFile)
	l.mu.Unlock()
}

// Exists checks if any mock files exist for a connector.
func (l *Loader) Exists(connector string) bool {
	path := filepath.Join(l.basePath, "connectors", connector)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// normalizeTarget converts a target/operation to a valid filename.
func normalizeTarget(target string) string {
	// Replace common separators with underscores
	result := strings.ReplaceAll(target, "/", "_")
	result = strings.ReplaceAll(result, ":", "")
	result = strings.ReplaceAll(result, " ", "_")
	result = strings.TrimPrefix(result, "_")

	// Handle special characters
	result = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, result)

	return result
}

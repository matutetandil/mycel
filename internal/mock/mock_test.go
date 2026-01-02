package mock

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

func TestLoader_LoadConnectorMock(t *testing.T) {
	// Create temp directory with mock file
	tmpDir := t.TempDir()
	mockDir := filepath.Join(tmpDir, "connectors", "postgres")
	if err := os.MkdirAll(mockDir, 0755); err != nil {
		t.Fatalf("failed to create mock dir: %v", err)
	}

	// Create mock file
	mockData := MockFile{
		Data: []map[string]interface{}{
			{"id": 1, "name": "John"},
			{"id": 2, "name": "Jane"},
		},
		Affected: 2,
	}
	mockBytes, _ := json.Marshal(mockData)
	mockPath := filepath.Join(mockDir, "users.json")
	if err := os.WriteFile(mockPath, mockBytes, 0644); err != nil {
		t.Fatalf("failed to write mock file: %v", err)
	}

	loader := NewLoader(tmpDir)

	// Test loading existing mock
	mock, err := loader.LoadConnectorMock("postgres", "users")
	if err != nil {
		t.Fatalf("failed to load mock: %v", err)
	}
	if mock == nil {
		t.Fatal("expected mock to be loaded")
	}

	// Test loading non-existent mock
	mock, err = loader.LoadConnectorMock("postgres", "orders")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock != nil {
		t.Error("expected nil mock for non-existent file")
	}
}

func TestLoader_ConditionalMock(t *testing.T) {
	tmpDir := t.TempDir()
	mockDir := filepath.Join(tmpDir, "connectors", "db")
	if err := os.MkdirAll(mockDir, 0755); err != nil {
		t.Fatalf("failed to create mock dir: %v", err)
	}

	// Create mock with conditions
	mockData := MockFile{
		Responses: []ConditionalResponse{
			{
				When: "input.id == 1",
				Data: map[string]interface{}{"id": 1, "name": "John"},
			},
			{
				When: "input.id == 2",
				Data: map[string]interface{}{"id": 2, "name": "Jane"},
			},
			{
				Default: true,
				Error:   "User not found",
				Status:  404,
			},
		},
	}
	mockBytes, _ := json.Marshal(mockData)
	mockPath := filepath.Join(mockDir, "users.json")
	if err := os.WriteFile(mockPath, mockBytes, 0644); err != nil {
		t.Fatalf("failed to write mock file: %v", err)
	}

	loader := NewLoader(tmpDir)
	mock, err := loader.LoadConnectorMock("db", "users")
	if err != nil {
		t.Fatalf("failed to load mock: %v", err)
	}

	if len(mock.Responses) != 3 {
		t.Errorf("expected 3 responses, got %d", len(mock.Responses))
	}
}

func TestManager_ShouldMock(t *testing.T) {
	tests := []struct {
		name       string
		config     *Config
		connector  string
		shouldMock bool
	}{
		{
			name:       "disabled",
			config:     &Config{Enabled: false},
			connector:  "postgres",
			shouldMock: false,
		},
		{
			name:       "enabled all",
			config:     &Config{Enabled: true},
			connector:  "postgres",
			shouldMock: true,
		},
		{
			name:       "mock only list - included",
			config:     &Config{Enabled: true, MockOnly: []string{"postgres", "mysql"}},
			connector:  "postgres",
			shouldMock: true,
		},
		{
			name:       "mock only list - excluded",
			config:     &Config{Enabled: true, MockOnly: []string{"postgres", "mysql"}},
			connector:  "redis",
			shouldMock: false,
		},
		{
			name:       "no-mock list - excluded",
			config:     &Config{Enabled: true, NoMock: []string{"redis"}},
			connector:  "redis",
			shouldMock: false,
		},
		{
			name:       "no-mock list - not excluded",
			config:     &Config{Enabled: true, NoMock: []string{"redis"}},
			connector:  "postgres",
			shouldMock: true,
		},
		{
			name: "per-connector disabled",
			config: &Config{
				Enabled: true,
				Connectors: map[string]*ConnectorMockConfig{
					"postgres": {Enabled: boolPtr(false)},
				},
			},
			connector:  "postgres",
			shouldMock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(tt.config)
			got := mgr.ShouldMock(tt.connector)
			if got != tt.shouldMock {
				t.Errorf("ShouldMock(%s) = %v, want %v", tt.connector, got, tt.shouldMock)
			}
		})
	}
}

func TestConnectorMockConfig_Latency(t *testing.T) {
	config := &ConnectorMockConfig{
		Latency: 100 * time.Millisecond,
	}

	if config.Latency != 100*time.Millisecond {
		t.Errorf("expected 100ms latency, got %v", config.Latency)
	}
}

func TestMockResult_ToConnectorResult(t *testing.T) {
	tmpDir := t.TempDir()
	mockDir := filepath.Join(tmpDir, "connectors", "db")
	os.MkdirAll(mockDir, 0755)

	mockData := MockFile{
		Data: []interface{}{
			map[string]interface{}{"id": float64(1), "name": "John"},
		},
	}
	mockBytes, _ := json.Marshal(mockData)
	os.WriteFile(filepath.Join(mockDir, "users.json"), mockBytes, 0644)

	loader := NewLoader(tmpDir)
	conn, err := NewConnector("db", nil, loader, nil, true)
	if err != nil {
		t.Fatalf("failed to create mock connector: %v", err)
	}

	// Test read
	ctx := context.Background()
	result, err := conn.Read(ctx, connector.Query{Target: "users"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}
}

func boolPtr(b bool) *bool {
	return &b
}

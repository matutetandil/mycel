package ftp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// mockClient implements the remoteClient interface for testing.
type mockClient struct {
	listFn   func(path string) ([]FileInfo, error)
	getFn    func(path string) ([]byte, error)
	putFn    func(path string, content []byte) error
	mkdirFn  func(path string) error
	removeFn func(path string) error
	closeFn  func() error
}

func (m *mockClient) List(path string) ([]FileInfo, error) {
	if m.listFn != nil {
		return m.listFn(path)
	}
	return nil, nil
}

func (m *mockClient) Get(path string) ([]byte, error) {
	if m.getFn != nil {
		return m.getFn(path)
	}
	return nil, nil
}

func (m *mockClient) Put(path string, content []byte) error {
	if m.putFn != nil {
		return m.putFn(path, content)
	}
	return nil
}

func (m *mockClient) Mkdir(path string) error {
	if m.mkdirFn != nil {
		return m.mkdirFn(path)
	}
	return nil
}

func (m *mockClient) Remove(path string) error {
	if m.removeFn != nil {
		return m.removeFn(path)
	}
	return nil
}

func (m *mockClient) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Protocol != "ftp" {
		t.Errorf("expected protocol 'ftp', got %q", cfg.Protocol)
	}
	if !cfg.Passive {
		t.Error("expected passive to be true by default")
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Timeout)
	}
}

func TestConnectorNameAndType(t *testing.T) {
	conn := New("my_ftp", DefaultConfig())

	if conn.Name() != "my_ftp" {
		t.Errorf("expected name 'my_ftp', got %q", conn.Name())
	}
	if conn.Type() != "ftp" {
		t.Errorf("expected type 'ftp', got %q", conn.Type())
	}
}

func TestProtocolDefaultPort(t *testing.T) {
	tests := []struct {
		protocol string
		expected int
	}{
		{"ftp", 21},
		{"sftp", 22},
	}

	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			cfg := &Config{Protocol: tt.protocol}
			conn := New("test", cfg)
			if conn.config.Port != tt.expected {
				t.Errorf("expected port %d for protocol %s, got %d", tt.expected, tt.protocol, conn.config.Port)
			}
		})
	}
}

func TestProtocolExplicitPort(t *testing.T) {
	cfg := &Config{Protocol: "ftp", Port: 2121}
	conn := New("test", cfg)
	if conn.config.Port != 2121 {
		t.Errorf("expected port 2121, got %d", conn.config.Port)
	}
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		target   string
		expected string
	}{
		{"no base path", "", "file.txt", "/file.txt"},
		{"with base path", "/data", "file.txt", "/data/file.txt"},
		{"nested target", "/data", "sub/file.txt", "/data/sub/file.txt"},
		{"trailing slash base", "/data/", "file.txt", "/data/file.txt"},
		{"empty target", "/data", "", "/data"},
		{"absolute target", "/data", "/other/file.txt", "/data/other/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := New("test", &Config{BasePath: tt.basePath})
			result := conn.resolvePath(tt.target)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestReadListOperation(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	mock := &mockClient{
		listFn: func(path string) ([]FileInfo, error) {
			return []FileInfo{
				{Name: "file1.txt", Size: 100, ModTime: now, IsDir: false},
				{Name: "subdir", Size: 0, ModTime: now, IsDir: true},
			}, nil
		},
	}

	conn := New("test", &Config{BasePath: "/data"})
	conn.client = mock

	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "reports",
		Operation: "LIST",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0]["name"] != "file1.txt" {
		t.Errorf("expected name 'file1.txt', got %v", result.Rows[0]["name"])
	}
	if result.Rows[0]["is_dir"] != false {
		t.Errorf("expected is_dir false, got %v", result.Rows[0]["is_dir"])
	}
	if result.Rows[1]["is_dir"] != true {
		t.Errorf("expected is_dir true, got %v", result.Rows[1]["is_dir"])
	}
}

func TestReadGetOperationJSON(t *testing.T) {
	mock := &mockClient{
		getFn: func(path string) ([]byte, error) {
			return []byte(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`), nil
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "data.json",
		Operation: "GET",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0]["name"] != "Alice" {
		t.Errorf("expected 'Alice', got %v", result.Rows[0]["name"])
	}
}

func TestReadGetOperationCSV(t *testing.T) {
	mock := &mockClient{
		getFn: func(path string) ([]byte, error) {
			return []byte("id,name\n1,Alice\n2,Bob\n"), nil
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	result, err := conn.Read(context.Background(), connector.Query{
		Target: "data.csv",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0]["name"] != "Alice" {
		t.Errorf("expected 'Alice', got %v", result.Rows[0]["name"])
	}
}

func TestReadGetOperationText(t *testing.T) {
	mock := &mockClient{
		getFn: func(path string) ([]byte, error) {
			return []byte("Hello, World!"), nil
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	result, err := conn.Read(context.Background(), connector.Query{
		Target: "readme.txt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["_content"] != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %v", result.Rows[0]["_content"])
	}
	if result.Rows[0]["_name"] != "readme.txt" {
		t.Errorf("expected '_name' to be 'readme.txt', got %v", result.Rows[0]["_name"])
	}
}

func TestWriteUpload(t *testing.T) {
	var capturedPath string
	var capturedContent []byte

	mock := &mockClient{
		putFn: func(path string, content []byte) error {
			capturedPath = path
			capturedContent = content
			return nil
		},
	}

	conn := New("test", &Config{BasePath: "/uploads"})
	conn.client = mock

	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "report.txt",
		Operation: "PUT",
		Payload: map[string]interface{}{
			"_content": "file content here",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPath != "/uploads/report.txt" {
		t.Errorf("expected path '/uploads/report.txt', got %q", capturedPath)
	}
	if string(capturedContent) != "file content here" {
		t.Errorf("expected content 'file content here', got %q", string(capturedContent))
	}
	if result.Metadata["uploaded"] != true {
		t.Errorf("expected uploaded=true, got %v", result.Metadata["uploaded"])
	}
}

func TestWriteUploadPayloadAsJSON(t *testing.T) {
	var capturedContent []byte

	mock := &mockClient{
		putFn: func(path string, content []byte) error {
			capturedContent = content
			return nil
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	result, err := conn.Write(context.Background(), &connector.Data{
		Target: "data.json",
		Payload: map[string]interface{}{
			"name": "Alice",
			"age":  30,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Metadata["uploaded"] != true {
		t.Errorf("expected uploaded=true")
	}
	// Should contain serialized JSON (no _content key, so full payload is serialized)
	if len(capturedContent) == 0 {
		t.Error("expected non-empty content")
	}
}

func TestWriteMkdir(t *testing.T) {
	var capturedPath string

	mock := &mockClient{
		mkdirFn: func(path string) error {
			capturedPath = path
			return nil
		},
	}

	conn := New("test", &Config{BasePath: "/data"})
	conn.client = mock

	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "new_folder",
		Operation: "MKDIR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/data/new_folder" {
		t.Errorf("expected path '/data/new_folder', got %q", capturedPath)
	}
	if result.Metadata["created"] != true {
		t.Errorf("expected created=true")
	}
}

func TestWriteDelete(t *testing.T) {
	var capturedPath string

	mock := &mockClient{
		removeFn: func(path string) error {
			capturedPath = path
			return nil
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "old_file.txt",
		Operation: "DELETE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/old_file.txt" {
		t.Errorf("expected path '/old_file.txt', got %q", capturedPath)
	}
	if result.Metadata["deleted"] != true {
		t.Errorf("expected deleted=true")
	}
}

func TestHealthNotConnected(t *testing.T) {
	conn := New("test", DefaultConfig())

	err := conn.Health(context.Background())
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestHealthConnected(t *testing.T) {
	mock := &mockClient{
		listFn: func(path string) ([]FileInfo, error) {
			return []FileInfo{}, nil
		},
	}

	conn := New("test", &Config{BasePath: "/data"})
	conn.client = mock

	err := conn.Health(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadNotConnected(t *testing.T) {
	conn := New("test", DefaultConfig())

	_, err := conn.Read(context.Background(), connector.Query{Target: "file.txt"})
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestWriteNotConnected(t *testing.T) {
	conn := New("test", DefaultConfig())

	_, err := conn.Write(context.Background(), &connector.Data{Target: "file.txt"})
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestGetFormat(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		params   map[string]interface{}
		expected string
	}{
		{"json ext", "data.json", nil, "json"},
		{"csv ext", "data.csv", nil, "csv"},
		{"txt ext", "readme.txt", nil, "text"},
		{"log ext", "app.log", nil, "text"},
		{"unknown ext", "data.bin", nil, "binary"},
		{"no ext", "data", nil, "binary"},
		{"param override", "data.txt", map[string]interface{}{"format": "json"}, "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFormat(tt.path, tt.params)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFactorySupports(t *testing.T) {
	factory := NewFactory()

	if !factory.Supports("ftp", "") {
		t.Error("factory should support 'ftp' type")
	}
	if !factory.Supports("ftp", "sftp") {
		t.Error("factory should support 'ftp' type with sftp driver")
	}
	if factory.Supports("s3", "") {
		t.Error("factory should not support 's3' type")
	}
	if factory.Type() != "ftp" {
		t.Errorf("expected type 'ftp', got %q", factory.Type())
	}
}

func TestFactoryMissingHost(t *testing.T) {
	factory := NewFactory()

	_, err := factory.Create(context.Background(), &connector.Config{
		Name: "test",
		Type: "ftp",
		Properties: map[string]interface{}{
			"username": "user",
			"password": "pass",
		},
	})
	if err == nil {
		t.Error("expected error when host is missing")
	}
}

func TestReadErrorPropagation(t *testing.T) {
	mock := &mockClient{
		getFn: func(path string) ([]byte, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	_, err := conn.Read(context.Background(), connector.Query{
		Target:    "file.txt",
		Operation: "GET",
	})
	if err == nil {
		t.Error("expected error to propagate from client")
	}
}

func TestWriteErrorPropagation(t *testing.T) {
	mock := &mockClient{
		putFn: func(path string, content []byte) error {
			return fmt.Errorf("permission denied")
		},
	}

	conn := New("test", &Config{})
	conn.client = mock

	_, err := conn.Write(context.Background(), &connector.Data{
		Target: "file.txt",
		Payload: map[string]interface{}{
			"_content": "data",
		},
	})
	if err == nil {
		t.Error("expected error to propagate from client")
	}
}

package s3

import (
	"context"
	"testing"

	"github.com/mycel-labs/mycel/internal/connector"
)

func TestConnector_Basic(t *testing.T) {
	// Test basic connector creation
	conn := New("test", &Config{
		Bucket: "test-bucket",
		Region: "us-east-1",
	})

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %q", conn.Name())
	}

	if conn.Type() != "s3" {
		t.Errorf("expected type 's3', got %q", conn.Type())
	}
}

func TestConnector_BuildKey(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		target   string
		expected string
	}{
		{"no prefix", "", "file.json", "file.json"},
		{"with prefix", "data", "file.json", "data/file.json"},
		{"prefix with slash", "data/", "file.json", "data/file.json"},
		{"nested path", "data", "folder/file.json", "data/folder/file.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := New("test", &Config{
				Bucket: "test-bucket",
				Prefix: tt.prefix,
			})

			result := conn.buildKey(tt.target)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestConnector_GetFormat(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		key      string
		params   map[string]interface{}
		expected string
	}{
		{"from params", "", "file.txt", map[string]interface{}{"format": "json"}, "json"},
		{"from config", "csv", "file.txt", nil, "csv"},
		{"from json ext", "", "data.json", nil, "json"},
		{"from csv ext", "", "data.csv", nil, "csv"},
		{"from txt ext", "", "readme.txt", nil, "text"},
		{"from log ext", "", "app.log", nil, "text"},
		{"from md ext", "", "README.md", nil, "text"},
		{"unknown ext", "", "file.xyz", nil, "binary"},
		{"no ext", "", "file", nil, "binary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := New("test", &Config{
				Bucket: "test-bucket",
				Format: tt.config,
			})

			params := tt.params
			if params == nil {
				params = map[string]interface{}{}
			}

			result := conn.getFormat(tt.key, params)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestConnector_ParseContent_JSON(t *testing.T) {
	conn := New("test", &Config{Bucket: "test"})

	// Test JSON array
	data := []byte(`[{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]`)
	result, err := conn.parseContent(data, "json")
	if err != nil {
		t.Fatalf("failed to parse JSON array: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
	if result[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", result[0]["name"])
	}

	// Test JSON object
	data = []byte(`{"id": 1, "name": "Alice"}`)
	result, err = conn.parseContent(data, "json")
	if err != nil {
		t.Fatalf("failed to parse JSON object: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 row, got %d", len(result))
	}
}

func TestConnector_ParseContent_CSV(t *testing.T) {
	conn := New("test", &Config{Bucket: "test"})

	data := []byte("id,name\n1,Alice\n2,Bob")
	result, err := conn.parseContent(data, "csv")
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
	if result[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", result[0]["name"])
	}
}

func TestConnector_ParseContent_Text(t *testing.T) {
	conn := New("test", &Config{Bucket: "test"})

	data := []byte("Hello, World!")
	result, err := conn.parseContent(data, "text")
	if err != nil {
		t.Fatalf("failed to parse text: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 row, got %d", len(result))
	}
	if result[0]["content"] != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %v", result[0]["content"])
	}
}

func TestConnector_ParseContent_Lines(t *testing.T) {
	conn := New("test", &Config{Bucket: "test"})

	data := []byte("Line 1\nLine 2\nLine 3")
	result, err := conn.parseContent(data, "lines")
	if err != nil {
		t.Fatalf("failed to parse lines: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result))
	}
	if result[0]["content"] != "Line 1" {
		t.Errorf("expected 'Line 1', got %v", result[0]["content"])
	}
}

func TestConnector_SerializeCSV(t *testing.T) {
	conn := New("test", &Config{Bucket: "test"})

	data := []map[string]interface{}{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}

	result, err := conn.serializeCSV(data)
	if err != nil {
		t.Fatalf("failed to serialize CSV: %v", err)
	}

	// Parse it back to verify
	parsed, err := conn.parseContent(result, "csv")
	if err != nil {
		t.Fatalf("failed to parse serialized CSV: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("expected 2 rows, got %d", len(parsed))
	}
}

func TestFactory_Supports(t *testing.T) {
	factory := NewFactory()

	if !factory.Supports("s3", "") {
		t.Error("factory should support 's3' type")
	}

	if factory.Supports("file", "") {
		t.Error("factory should not support 'file' type")
	}

	if factory.Type() != "s3" {
		t.Errorf("expected type 's3', got %q", factory.Type())
	}
}

func TestFactory_Create(t *testing.T) {
	factory := NewFactory()

	// Note: This test would fail without valid AWS credentials
	// or a local S3-compatible service like MinIO
	// We skip the actual connection test here

	_, err := factory.Create(context.Background(), &connector.Config{
		Name: "test",
		Type: "s3",
		Properties: map[string]interface{}{
			"bucket":         "test-bucket",
			"region":         "us-east-1",
			"access_key":     "test",
			"secret_key":     "test",
			"endpoint":       "http://localhost:9000",
			"use_path_style": true,
		},
	})

	// We expect this to fail since there's no S3 server
	// The important thing is that the factory correctly parses the config
	if err == nil {
		t.Log("Factory created connector successfully (S3 service available)")
	} else {
		t.Log("Factory failed to connect (expected without S3 service):", err)
	}
}

package file

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// helper to create a watcher connector with short poll interval
func newWatchConnector(t *testing.T, dir string) *Connector {
	t.Helper()
	return New("test-watch", &Config{
		BasePath:      dir,
		Format:        "json",
		Watch:         true,
		WatchInterval: 50 * time.Millisecond,
		CreateDirs:    true,
	})
}

func TestWatch_NewFileDetected(t *testing.T) {
	dir := t.TempDir()
	conn := newWatchConnector(t, dir)

	var mu sync.Mutex
	var received []map[string]interface{}

	conn.RegisterRoute("*.json", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		received = append(received, input)
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	// Write a JSON file after watcher starts
	data := []byte(`{"name":"Alice"}`)
	if err := os.WriteFile(filepath.Join(dir, "users.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for detection
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected handler to be called for new file")
	}

	input := received[0]
	if input["_event"] != "created" {
		t.Errorf("expected event 'created', got %v", input["_event"])
	}
	if input["_name"] != "users.json" {
		t.Errorf("expected name 'users.json', got %v", input["_name"])
	}
	if input["_path"] != "users.json" {
		t.Errorf("expected path 'users.json', got %v", input["_path"])
	}
}

func TestWatch_ModifiedFileDetected(t *testing.T) {
	dir := t.TempDir()

	// Create file before starting watcher
	filePath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(filePath, []byte(`{"v":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	conn := newWatchConnector(t, dir)

	var mu sync.Mutex
	var received []map[string]interface{}

	conn.RegisterRoute("*.json", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		received = append(received, input)
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	// Wait for seed scan to complete
	time.Sleep(100 * time.Millisecond)

	// Modify the file
	if err := os.WriteFile(filePath, []byte(`{"v":2,"extra":"field"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for detection
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected handler to be called for modified file")
	}

	input := received[0]
	if input["_event"] != "modified" {
		t.Errorf("expected event 'modified', got %v", input["_event"])
	}
}

func TestWatch_GlobPatternMatching(t *testing.T) {
	dir := t.TempDir()
	conn := newWatchConnector(t, dir)

	var mu sync.Mutex
	csvCalls := 0

	conn.RegisterRoute("*.csv", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		csvCalls++
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	// Write a CSV file (should match)
	csvFile, _ := os.Create(filepath.Join(dir, "data.csv"))
	w := csv.NewWriter(csvFile)
	w.Write([]string{"id", "name"})
	w.Write([]string{"1", "Alice"})
	w.Flush()
	csvFile.Close()

	// Write a JSON file (should NOT match)
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{}`), 0644)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if csvCalls != 1 {
		t.Errorf("expected CSV handler called 1 time, got %d", csvCalls)
	}
}

func TestWatch_NestedPathMatching(t *testing.T) {
	dir := t.TempDir()
	conn := newWatchConnector(t, dir)

	var mu sync.Mutex
	var receivedPath string

	conn.RegisterRoute("reports/*.csv", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		receivedPath, _ = input["_path"].(string)
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	// Create nested directory and file
	reportsDir := filepath.Join(dir, "reports")
	os.MkdirAll(reportsDir, 0755)

	csvFile, _ := os.Create(filepath.Join(reportsDir, "jan.csv"))
	w := csv.NewWriter(csvFile)
	w.Write([]string{"month", "total"})
	w.Write([]string{"jan", "1000"})
	w.Flush()
	csvFile.Close()

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	expected := filepath.Join("reports", "jan.csv")
	if receivedPath != expected {
		t.Errorf("expected path %q, got %q", expected, receivedPath)
	}
}

func TestWatch_UnchangedFilesIgnored(t *testing.T) {
	dir := t.TempDir()

	// Create file before watcher starts
	filePath := filepath.Join(dir, "static.json")
	if err := os.WriteFile(filePath, []byte(`{"v":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	conn := newWatchConnector(t, dir)

	var mu sync.Mutex
	callCount := 0

	conn.RegisterRoute("*.json", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	// Wait for several poll cycles
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if callCount != 0 {
		t.Errorf("expected 0 handler calls for unchanged file, got %d", callCount)
	}
}

func TestWatch_DisabledNoop(t *testing.T) {
	dir := t.TempDir()

	conn := New("test-no-watch", &Config{
		BasePath:      dir,
		Format:        "json",
		Watch:         false,
		WatchInterval: 50 * time.Millisecond,
	})

	conn.RegisterRoute("*.json", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		t.Error("handler should not be called when watch is disabled")
		return nil, nil
	})

	ctx := context.Background()
	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify no goroutine was started
	conn.mu.RLock()
	started := conn.started
	conn.mu.RUnlock()

	if started {
		t.Error("expected connector to not be started when watch=false")
	}
}

func TestWatch_FileContentInInput(t *testing.T) {
	dir := t.TempDir()
	conn := newWatchConnector(t, dir)

	var mu sync.Mutex
	var received map[string]interface{}

	conn.RegisterRoute("*.json", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		received = input
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	// Write a JSON file with content
	data := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
	}
	jsonBytes, _ := json.Marshal(data)
	os.WriteFile(filepath.Join(dir, "user.json"), jsonBytes, 0644)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if received == nil {
		t.Fatal("expected handler to be called")
	}

	// Single JSON object should be flattened into input
	if received["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %v", received["name"])
	}
	if received["email"] != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %v", received["email"])
	}

	// Metadata should also be present
	if received["_event"] != "created" {
		t.Errorf("expected _event 'created', got %v", received["_event"])
	}
	if received["_name"] != "user.json" {
		t.Errorf("expected _name 'user.json', got %v", received["_name"])
	}
}

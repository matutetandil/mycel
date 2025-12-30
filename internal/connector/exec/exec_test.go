package exec

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

func TestConnector_LocalExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "echo",
		Args:         []string{"hello", "world"},
		Timeout:      5 * time.Second,
		OutputFormat: "text",
	}

	conn := New("test-echo", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	result, err := conn.Read(ctx, connector.Query{})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}

	output, ok := result.Rows[0]["output"].(string)
	if !ok {
		t.Fatal("Expected output field")
	}

	if output != "hello world\n" {
		t.Errorf("Expected 'hello world\\n', got '%s'", output)
	}
}

func TestConnector_JSONOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "echo",
		Args:         []string{`{"name":"test","value":42}`},
		Timeout:      5 * time.Second,
		OutputFormat: "json",
	}

	conn := New("test-json", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	result, err := conn.Read(ctx, connector.Query{})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}

	name, ok := result.Rows[0]["name"].(string)
	if !ok || name != "test" {
		t.Errorf("Expected name='test', got '%v'", result.Rows[0]["name"])
	}

	value, ok := result.Rows[0]["value"].(float64)
	if !ok || value != 42 {
		t.Errorf("Expected value=42, got '%v'", result.Rows[0]["value"])
	}
}

func TestConnector_JSONArrayOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "echo",
		Args:         []string{`[{"id":1},{"id":2}]`},
		Timeout:      5 * time.Second,
		OutputFormat: "json",
	}

	conn := New("test-json-array", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	result, err := conn.Read(ctx, connector.Query{})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("Expected 2 rows, got %d", len(result.Rows))
	}
}

func TestConnector_LinesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "printf",
		Args:         []string{"line1\\nline2\\nline3"},
		Timeout:      5 * time.Second,
		OutputFormat: "lines",
	}

	conn := New("test-lines", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	result, err := conn.Read(ctx, connector.Query{})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) != 3 {
		t.Fatalf("Expected 3 rows, got %d", len(result.Rows))
	}

	for i, row := range result.Rows {
		lineNum, ok := row["line"].(int)
		if !ok || lineNum != i+1 {
			t.Errorf("Expected line=%d, got '%v'", i+1, row["line"])
		}
	}
}

func TestConnector_ShellExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "echo $((2 + 2))",
		Shell:        "bash -c",
		Timeout:      5 * time.Second,
		OutputFormat: "text",
	}

	conn := New("test-shell", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	result, err := conn.Read(ctx, connector.Query{})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}

	output, ok := result.Rows[0]["output"].(string)
	if !ok {
		t.Fatal("Expected output field")
	}

	if output != "4\n" {
		t.Errorf("Expected '4\\n', got '%s'", output)
	}
}

func TestConnector_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:  "local",
		Command: "sleep",
		Args:    []string{"10"},
		Timeout: 100 * time.Millisecond,
	}

	conn := New("test-timeout", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	_, err = conn.Read(ctx, connector.Query{})
	if err == nil {
		t.Fatal("Expected timeout error")
	}

	if err.Error() != "command timed out after 100ms" {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

func TestConnector_Call(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "echo",
		Args:         []string{`{"price":99.99,"currency":"USD"}`},
		Timeout:      5 * time.Second,
		OutputFormat: "json",
	}

	conn := New("test-call", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	result, err := conn.Call(ctx, "", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Should return single object, not array
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map result, got %T", result)
	}

	price, ok := resultMap["price"].(float64)
	if !ok || price != 99.99 {
		t.Errorf("Expected price=99.99, got %v", resultMap["price"])
	}
}

func TestConnector_Write(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:      "local",
		Command:     "cat",
		Timeout:     5 * time.Second,
		InputFormat: "json",
	}

	conn := New("test-write", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	data := &connector.Data{
		Payload: map[string]interface{}{
			"message": "hello",
		},
	}

	result, err := conn.Write(ctx, data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if result.Affected != 1 {
		t.Errorf("Expected affected=1, got %d", result.Affected)
	}
}

func TestConnector_ArgsInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	config := &Config{
		Driver:       "local",
		Command:      "echo",
		Timeout:      5 * time.Second,
		InputFormat:  "args",
		OutputFormat: "text",
	}

	conn := New("test-args", config)

	ctx := context.Background()
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Use operation as the main argument
	result, err := conn.Read(ctx, connector.Query{
		Target: "hello",
		Filters: map[string]interface{}{
			"name": "test",
		},
	})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}

	output, ok := result.Rows[0]["output"].(string)
	if !ok {
		t.Fatal("Expected output field")
	}

	// Should contain the operation and args
	if output != "hello --name=test\n" {
		t.Errorf("Expected 'hello --name=test\\n', got '%s'", output)
	}
}

func TestConnector_Health(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	t.Run("command exists", func(t *testing.T) {
		config := &Config{
			Driver:  "local",
			Command: "echo",
		}

		conn := New("test-health", config)

		err := conn.Health(context.Background())
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("command not found", func(t *testing.T) {
		config := &Config{
			Driver:  "local",
			Command: "nonexistent-command-xyz",
		}

		conn := New("test-health-fail", config)

		err := conn.Health(context.Background())
		if err == nil {
			t.Error("Expected error for non-existent command")
		}
	})
}

func TestFactory_Create(t *testing.T) {
	factory := NewFactory(nil)

	if !factory.Supports("exec", "") {
		t.Error("Expected factory to support 'exec' type")
	}

	cfg := &connector.Config{
		Name:   "test-factory",
		Type:   "exec",
		Driver: "local",
		Properties: map[string]interface{}{
			"command":       "echo",
			"args":          []interface{}{"hello"},
			"timeout":       "10s",
			"output_format": "text",
			"env": map[string]interface{}{
				"MY_VAR": "my_value",
			},
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if conn.Name() != "test-factory" {
		t.Errorf("Expected name 'test-factory', got '%s'", conn.Name())
	}

	execConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("Expected *Connector type")
	}

	if execConn.config.Command != "echo" {
		t.Errorf("Expected command 'echo', got '%s'", execConn.config.Command)
	}

	if execConn.config.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", execConn.config.Timeout)
	}

	if execConn.config.Env["MY_VAR"] != "my_value" {
		t.Errorf("Expected MY_VAR='my_value', got '%s'", execConn.config.Env["MY_VAR"])
	}
}

func TestFactory_CreateSSH(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name:   "test-ssh",
		Type:   "exec",
		Driver: "ssh",
		Properties: map[string]interface{}{
			"command": "ls",
			"ssh": map[string]interface{}{
				"host":     "example.com",
				"port":     22,
				"user":     "admin",
				"key_file": "/path/to/key",
			},
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	execConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("Expected *Connector type")
	}

	if execConn.config.Driver != "ssh" {
		t.Errorf("Expected driver 'ssh', got '%s'", execConn.config.Driver)
	}

	if execConn.config.SSH == nil {
		t.Fatal("Expected SSH config")
	}

	if execConn.config.SSH.Host != "example.com" {
		t.Errorf("Expected host 'example.com', got '%s'", execConn.config.SSH.Host)
	}

	if execConn.config.SSH.User != "admin" {
		t.Errorf("Expected user 'admin', got '%s'", execConn.config.SSH.User)
	}
}

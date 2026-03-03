package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/xuri/excelize/v2"
)

func TestConnector_ReadWriteJSON(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create connector
	conn := New("test", &Config{
		BasePath:   tmpDir,
		Format:     "json",
		CreateDirs: true,
	})

	ctx := context.Background()

	// Write test data
	testData := []map[string]interface{}{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}

	result, err := conn.Write(ctx, &connector.Data{
		Target: "users.json",
		Params: map[string]interface{}{
			"content": testData,
		},
	})
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	if result["path"] != filepath.Join(tmpDir, "users.json") {
		t.Errorf("unexpected path: %v", result["path"])
	}

	// Read back
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "users.json",
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}
}

func TestConnector_ReadWriteCSV(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	conn := New("test", &Config{
		BasePath:   tmpDir,
		CreateDirs: true,
	})

	ctx := context.Background()

	// Write test data
	testData := []map[string]interface{}{
		{"id": "1", "name": "Alice"},
		{"id": "2", "name": "Bob"},
	}

	_, err = conn.Write(ctx, &connector.Data{
		Target: "users.csv",
		Params: map[string]interface{}{
			"content": testData,
			"format":  "csv",
		},
	})
	if err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	// Read back
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "users.csv",
	})
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
}

func TestConnector_ReadWriteText(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	conn := New("test", &Config{
		BasePath:   tmpDir,
		CreateDirs: true,
	})

	ctx := context.Background()

	// Write text
	_, err = conn.Write(ctx, &connector.Data{
		Target: "readme.txt",
		Params: map[string]interface{}{
			"content": "Hello, World!",
			"format":  "text",
		},
	})
	if err != nil {
		t.Fatalf("failed to write text: %v", err)
	}

	// Read back
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "readme.txt",
		Params: map[string]interface{}{
			"format": "text",
		},
	})
	if err != nil {
		t.Fatalf("failed to read text: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	if rows[0]["content"] != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %v", rows[0]["content"])
	}
}

func TestConnector_ListDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.json"), []byte("{}"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	conn := New("test", &Config{
		BasePath: tmpDir,
	})

	ctx := context.Background()

	rows, err := conn.Read(ctx, &connector.Query{
		Target: ".",
	})
	if err != nil {
		t.Fatalf("failed to list directory: %v", err)
	}

	if len(rows) != 3 {
		t.Errorf("expected 3 entries, got %d", len(rows))
	}
}

func TestConnector_FileOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	conn := New("test", &Config{
		BasePath:   tmpDir,
		CreateDirs: true,
	})

	ctx := context.Background()

	// Create a file
	os.WriteFile(filepath.Join(tmpDir, "original.txt"), []byte("test content"), 0644)

	// Test exists
	result, err := conn.Call(ctx, "exists", map[string]interface{}{
		"path": "original.txt",
	})
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !result.(map[string]interface{})["exists"].(bool) {
		t.Error("expected file to exist")
	}

	// Test stat
	result, err = conn.Call(ctx, "stat", map[string]interface{}{
		"path": "original.txt",
	})
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	stat := result.(map[string]interface{})
	if stat["name"] != "original.txt" {
		t.Errorf("expected name original.txt, got %v", stat["name"])
	}

	// Test copy
	result, err = conn.Call(ctx, "copy", map[string]interface{}{
		"source":      "original.txt",
		"destination": "copied.txt",
	})
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	if !result.(map[string]interface{})["copied"].(bool) {
		t.Error("expected copy to succeed")
	}

	// Verify copy exists
	if _, err := os.Stat(filepath.Join(tmpDir, "copied.txt")); err != nil {
		t.Error("copied file doesn't exist")
	}

	// Test move
	result, err = conn.Call(ctx, "move", map[string]interface{}{
		"source":      "copied.txt",
		"destination": "moved.txt",
	})
	if err != nil {
		t.Fatalf("move failed: %v", err)
	}
	if !result.(map[string]interface{})["moved"].(bool) {
		t.Error("expected move to succeed")
	}

	// Test delete
	result, err = conn.Call(ctx, "delete", map[string]interface{}{
		"path": "moved.txt",
	})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !result.(map[string]interface{})["deleted"].(bool) {
		t.Error("expected delete to succeed")
	}
}

func TestConnector_ReadWriteExcel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	conn := New("test", &Config{
		BasePath:   tmpDir,
		CreateDirs: true,
	})

	ctx := context.Background()

	// Write test data
	testData := []map[string]interface{}{
		{"id": "1", "name": "Alice", "email": "alice@example.com"},
		{"id": "2", "name": "Bob", "email": "bob@example.com"},
	}

	result, err := conn.Write(ctx, &connector.Data{
		Target: "users.xlsx",
		Params: map[string]interface{}{
			"content": testData,
		},
	})
	if err != nil {
		t.Fatalf("failed to write Excel: %v", err)
	}

	if result["path"] != filepath.Join(tmpDir, "users.xlsx") {
		t.Errorf("unexpected path: %v", result["path"])
	}

	// Read back
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "users.xlsx",
	})
	if err != nil {
		t.Fatalf("failed to read Excel: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Verify values (all Excel values come back as strings via GetRows)
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}
	if rows[1]["name"] != "Bob" {
		t.Errorf("expected Bob, got %v", rows[1]["name"])
	}
	if rows[0]["email"] != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %v", rows[0]["email"])
	}
}

func TestConnector_ReadExcelSheet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create an Excel file with a named sheet using excelize directly
	filePath := filepath.Join(tmpDir, "products.xlsx")
	f := excelize.NewFile()

	// Rename default sheet and add data
	f.SetSheetName("Sheet1", "Products")
	f.SetCellValue("Products", "A1", "sku")
	f.SetCellValue("Products", "B1", "price")
	f.SetCellValue("Products", "A2", "ABC-123")
	f.SetCellValue("Products", "B2", "29.99")
	f.SetCellValue("Products", "A3", "DEF-456")
	f.SetCellValue("Products", "B3", "49.99")

	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("failed to create test Excel: %v", err)
	}
	f.Close()

	conn := New("test", &Config{
		BasePath: tmpDir,
	})

	ctx := context.Background()

	// Read specific sheet
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "products.xlsx",
		Params: map[string]interface{}{
			"sheet": "Products",
		},
	})
	if err != nil {
		t.Fatalf("failed to read Excel sheet: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0]["sku"] != "ABC-123" {
		t.Errorf("expected ABC-123, got %v", rows[0]["sku"])
	}
	if rows[1]["price"] != "49.99" {
		t.Errorf("expected 49.99, got %v", rows[1]["price"])
	}
}

func TestConnector_AppendMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	conn := New("test", &Config{
		BasePath:   tmpDir,
		CreateDirs: true,
	})

	ctx := context.Background()

	// Write initial content
	_, err = conn.Write(ctx, &connector.Data{
		Target: "log.txt",
		Params: map[string]interface{}{
			"content": "Line 1\n",
			"format":  "text",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Append more content
	_, err = conn.Write(ctx, &connector.Data{
		Target: "log.txt",
		Params: map[string]interface{}{
			"content": "Line 2\n",
			"format":  "text",
			"append":  true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Read back
	content, err := os.ReadFile(filepath.Join(tmpDir, "log.txt"))
	if err != nil {
		t.Fatal(err)
	}

	expected := "Line 1\nLine 2\n"
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}
}

func TestFactory_Create(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	factory := NewFactory()

	if !factory.Supports("file", "") {
		t.Error("factory should support 'file' type")
	}

	conn, err := factory.Create(context.Background(), &connector.Config{
		Name: "test",
		Type: "file",
		Properties: map[string]interface{}{
			"base_path":   tmpDir,
			"format":      "json",
			"create_dirs": true,
		},
	})
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	if conn.Name() != "test" {
		t.Errorf("expected name 'test', got %q", conn.Name())
	}

	if conn.Type() != "file" {
		t.Errorf("expected type 'file', got %q", conn.Type())
	}
}

// Ensure json import is used
var _ = json.Marshal

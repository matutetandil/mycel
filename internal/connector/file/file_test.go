package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestConnector_ReadWriteTSV(t *testing.T) {
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

	// Write TSV manually
	tsvContent := "name\tage\tcity\nAlice\t30\tNYC\nBob\t25\tLA\n"
	os.WriteFile(filepath.Join(tmpDir, "data.tsv"), []byte(tsvContent), 0644)

	// Read back as TSV (auto-detected from extension)
	rows, err := conn.Read(ctx, &connector.Query{Target: "data.tsv"})
	if err != nil {
		t.Fatalf("failed to read TSV: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}
	if rows[0]["age"] != "30" {
		t.Errorf("expected 30, got %v", rows[0]["age"])
	}

	// Write TSV data
	testData := []map[string]interface{}{
		{"x": "1", "y": "2"},
		{"x": "3", "y": "4"},
	}
	_, err = conn.Write(ctx, &connector.Data{
		Target: "output.tsv",
		Params: map[string]interface{}{
			"content": testData,
		},
	})
	if err != nil {
		t.Fatalf("failed to write TSV: %v", err)
	}

	// Read back
	rows, err = conn.Read(ctx, &connector.Query{Target: "output.tsv"})
	if err != nil {
		t.Fatalf("failed to read written TSV: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
}

func TestConnector_CSVDelimiter(t *testing.T) {
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

	// Write a semicolon-delimited CSV
	content := "name;age;city\nAlice;30;NYC\nBob;25;LA\n"
	os.WriteFile(filepath.Join(tmpDir, "european.csv"), []byte(content), 0644)

	// Read with semicolon delimiter
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "european.csv",
		Params: map[string]interface{}{"delimiter": ";"},
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}
	if rows[1]["city"] != "LA" {
		t.Errorf("expected LA, got %v", rows[1]["city"])
	}
}

func TestConnector_CSVNoHeader(t *testing.T) {
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

	// Write CSV without header
	content := "Alice,30,NYC\nBob,25,LA\n"
	os.WriteFile(filepath.Join(tmpDir, "noheader.csv"), []byte(content), 0644)

	// Read without header
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "noheader.csv",
		Params: map[string]interface{}{"no_header": true},
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["column_1"] != "Alice" {
		t.Errorf("expected Alice in column_1, got %v", rows[0]["column_1"])
	}
	if rows[0]["column_2"] != "30" {
		t.Errorf("expected 30 in column_2, got %v", rows[0]["column_2"])
	}
}

func TestConnector_CSVCustomColumns(t *testing.T) {
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

	// Write CSV with header that we want to override
	content := "col_a,col_b,col_c\nAlice,30,NYC\nBob,25,LA\n"
	os.WriteFile(filepath.Join(tmpDir, "custom.csv"), []byte(content), 0644)

	// Read with custom column names (overrides the header row)
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "custom.csv",
		Params: map[string]interface{}{
			"columns": []interface{}{"name", "age", "city"},
		},
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice in name, got %v", rows[0]["name"])
	}
	if rows[1]["city"] != "LA" {
		t.Errorf("expected LA in city, got %v", rows[1]["city"])
	}
}

func TestConnector_CSVSkipRows(t *testing.T) {
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

	// Write CSV with metadata rows before the header
	content := "Report: Users\nGenerated: 2026-03-09\nname,age\nAlice,30\nBob,25\n"
	os.WriteFile(filepath.Join(tmpDir, "report.csv"), []byte(content), 0644)

	// Read skipping 2 rows (2 metadata lines)
	rows, err := conn.Read(ctx, &connector.Query{
		Target: "report.csv",
		Params: map[string]interface{}{"skip_rows": 2},
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}
}

func TestConnector_CSVComment(t *testing.T) {
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

	// Write CSV with comment lines
	content := "name,age\n# This is a comment\nAlice,30\n# Another comment\nBob,25\n"
	os.WriteFile(filepath.Join(tmpDir, "comments.csv"), []byte(content), 0644)

	rows, err := conn.Read(ctx, &connector.Query{
		Target: "comments.csv",
		Params: map[string]interface{}{"comment": "#"},
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (comments skipped), got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}
}

func TestConnector_CSVBOM(t *testing.T) {
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

	// Write CSV with UTF-8 BOM (common from Excel on Windows)
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := append(bom, []byte("name,age\nAlice,30\n")...)
	os.WriteFile(filepath.Join(tmpDir, "bom.csv"), content, 0644)

	rows, err := conn.Read(ctx, &connector.Query{Target: "bom.csv"})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// Without BOM handling, the first header would be "\xef\xbb\xbfname"
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice (BOM stripped), got headers: %v", rows[0])
	}
}

func TestConnector_CSVTrimSpace(t *testing.T) {
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

	content := " name , age \n Alice , 30 \n Bob , 25 \n"
	os.WriteFile(filepath.Join(tmpDir, "spaces.csv"), []byte(content), 0644)

	rows, err := conn.Read(ctx, &connector.Query{
		Target: "spaces.csv",
		Params: map[string]interface{}{"trim_space": true},
	})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected trimmed 'Alice', got %q", rows[0]["name"])
	}
	if rows[0]["age"] != "30" {
		t.Errorf("expected trimmed '30', got %q", rows[0]["age"])
	}
}

func TestConnector_CSVWriteColumnOrder(t *testing.T) {
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

	testData := []map[string]interface{}{
		{"name": "Alice", "age": "30", "city": "NYC"},
	}

	// Write with explicit column order
	_, err = conn.Write(ctx, &connector.Data{
		Target: "ordered.csv",
		Params: map[string]interface{}{
			"content": testData,
			"columns": []interface{}{"city", "name", "age"},
		},
	})
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read raw file to verify column order
	raw, _ := os.ReadFile(filepath.Join(tmpDir, "ordered.csv"))
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if lines[0] != "city,name,age" {
		t.Errorf("expected header 'city,name,age', got %q", lines[0])
	}
	if lines[1] != "NYC,Alice,30" {
		t.Errorf("expected data 'NYC,Alice,30', got %q", lines[1])
	}
}

func TestConnector_CSVConnectorDefaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-file-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create connector with semicolon delimiter as default
	conn := New("test", &Config{
		BasePath:   tmpDir,
		CreateDirs: true,
		CSV: CSVOptions{
			Delimiter: ';',
			TrimSpace: true,
		},
	})

	ctx := context.Background()

	content := "name ; age\n Alice ; 30\n"
	os.WriteFile(filepath.Join(tmpDir, "default.csv"), []byte(content), 0644)

	rows, err := conn.Read(ctx, &connector.Query{Target: "default.csv"})
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected 'Alice', got %q", rows[0]["name"])
	}
}

// Ensure json import is used
var _ = json.Marshal

package pdf

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

func TestGenerateBytes_SimpleHTML(t *testing.T) {
	c := New("test", &Config{})

	html := `<h1>Hello World</h1><p>This is a test.</p>`
	bytes, err := c.GenerateBytes(html, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}

	// PDF files start with %PDF
	if !strings.HasPrefix(string(bytes), "%PDF") {
		t.Error("expected PDF header (%PDF)")
	}
}

func TestGenerateBytes_WithTemplate(t *testing.T) {
	c := New("test", &Config{})

	html := `<h1>Invoice #{{.number}}</h1>
<p>Customer: {{.customer}}</p>
<p>Total: ${{.total}}</p>`

	data := map[string]interface{}{
		"number":   "INV-001",
		"customer": "Alice",
		"total":    "150.00",
	}

	bytes, err := c.GenerateBytes(html, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}
}

func TestGenerateBytes_Table(t *testing.T) {
	c := New("test", &Config{})

	html := `<table>
<tr><th>Item</th><th>Price</th></tr>
<tr><td>Widget</td><td>$10</td></tr>
<tr><td>Gadget</td><td>$25</td></tr>
</table>`

	bytes, err := c.GenerateBytes(html, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}
}

func TestGenerateBytes_List(t *testing.T) {
	c := New("test", &Config{})

	html := `<h2>Items</h2>
<ul>
<li>First item</li>
<li>Second item</li>
<li>Third item</li>
</ul>
<ol>
<li>Step one</li>
<li>Step two</li>
</ol>`

	bytes, err := c.GenerateBytes(html, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}
}

func TestGenerateBytes_Styles(t *testing.T) {
	c := New("test", &Config{})

	html := `<h1 style="text-align: center; color: #336699">Centered Title</h1>
<p style="text-align: right; font-size: 18px">Right aligned large text</p>
<hr>
<p>Normal paragraph with <strong>bold</strong> and <em>italic</em> text.</p>`

	bytes, err := c.GenerateBytes(html, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}
}

func TestGenerateBytes_TemplateRange(t *testing.T) {
	c := New("test", &Config{})

	html := `<h1>Invoice</h1>
<table>
<tr><th>Item</th><th>Qty</th><th>Price</th></tr>
{{range .items}}<tr><td>{{.name}}</td><td>{{.qty}}</td><td>${{.price}}</td></tr>
{{end}}</table>`

	data := map[string]interface{}{
		"items": []map[string]interface{}{
			{"name": "Widget", "qty": "2", "price": "10.00"},
			{"name": "Gadget", "qty": "1", "price": "25.00"},
		},
	}

	bytes, err := c.GenerateBytes(html, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}
}

func TestWrite_Generate(t *testing.T) {
	// Create a temp template file
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.html")
	os.WriteFile(tmplPath, []byte(`<h1>Hello {{.name}}</h1>`), 0644)

	c := New("test", &Config{})

	result, err := c.Write(context.Background(), &connector.Data{
		Operation: "generate",
		Payload: map[string]interface{}{
			"template": tmplPath,
			"name":     "World",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Affected != 1 {
		t.Errorf("expected affected=1, got %d", result.Affected)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	row := result.Rows[0]

	// Check content type
	ct, _ := row["_content_type"].(string)
	if ct != "application/pdf" {
		t.Errorf("expected content_type 'application/pdf', got %q", ct)
	}

	// Check binary is base64
	b64, _ := row["_binary"].(string)
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}
	if !strings.HasPrefix(string(decoded), "%PDF") {
		t.Error("expected PDF header in decoded binary")
	}
}

func TestWrite_Save(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.html")
	os.WriteFile(tmplPath, []byte(`<h1>Saved PDF</h1>`), 0644)

	c := New("test", &Config{OutputDir: dir})

	result, err := c.Write(context.Background(), &connector.Data{
		Operation: "save",
		Payload: map[string]interface{}{
			"template": tmplPath,
			"filename": "output.pdf",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	row := result.Rows[0]
	filePath, _ := row["file_path"].(string)

	// Verify file exists
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}

	// Verify PDF header
	content, _ := os.ReadFile(filePath)
	if !strings.HasPrefix(string(content), "%PDF") {
		t.Error("expected PDF header in saved file")
	}
}

func TestWrite_MissingTemplate(t *testing.T) {
	c := New("test", &Config{})

	_, err := c.Write(context.Background(), &connector.Data{
		Operation: "generate",
		Payload:   map[string]interface{}{},
	})
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestWrite_InvalidOperation(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.html")
	os.WriteFile(tmplPath, []byte(`<p>test</p>`), 0644)

	c := New("test", &Config{})

	_, err := c.Write(context.Background(), &connector.Data{
		Operation: "invalid",
		Payload: map[string]interface{}{
			"template": tmplPath,
		},
	})
	if err == nil {
		t.Error("expected error for invalid operation")
	}
}

func TestPageSize_Letter(t *testing.T) {
	c := New("test", &Config{PageSize: "Letter"})

	bytes, err := c.GenerateBytes(`<h1>Letter Size</h1>`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}
}

var data = map[string]interface{}{} // shared empty data for tests

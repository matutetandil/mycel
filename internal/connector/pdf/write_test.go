package pdf

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Turning data into a document.
//
// An invoice, a packing slip, a statement: something a customer receives, so
// it either arrives or it does not. What it is called comes from the payload —
// an invoice number, an order reference — which means the name is data, and
// often data from outside.

func templateIn(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invoice.html")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestADocumentIsGeneratedFromATemplate(t *testing.T) {
	template := templateIn(t, `<h1>Invoice {{.number}}</h1><p>Total: {{.total}}</p>`)
	c := New("invoices", &Config{Template: template})

	result, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"number": "INV-1", "total": "42.50"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	row := result.Rows[0]
	// The document comes back encoded, because a REST flow answers with it
	// and JSON has no way to carry bytes.
	encoded, ok := row["_binary"].(string)
	if !ok || encoded == "" {
		t.Fatalf("no document came back: %v", row)
	}
	document, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the document is not decodable: %v", err)
	}
	// A PDF says what it is in its first bytes; anything else is a file no
	// reader will open.
	if !strings.HasPrefix(string(document), "%PDF-") {
		t.Errorf("what came back is not a PDF: %q", string(document[:min(8, len(document))]))
	}
	// The content type and the filename are what make a browser offer to
	// save it rather than render it as text.
	if row["_content_type"] != "application/pdf" {
		t.Errorf("content type = %v", row["_content_type"])
	}
	if row["_filename"] != "document.pdf" {
		t.Errorf("filename = %v", row["_filename"])
	}
}

func TestADocumentSavedToDisk(t *testing.T) {
	template := templateIn(t, `<h1>Invoice {{.number}}</h1>`)
	outputDir := t.TempDir()
	c := New("invoices", &Config{Template: template, OutputDir: outputDir})

	result, err := c.Write(context.Background(), &connector.Data{
		Operation: "save",
		Payload:   map[string]interface{}{"number": "INV-1", "filename": "INV-1.pdf"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	path, _ := result.Rows[0]["file_path"].(string)
	if path == "" {
		t.Fatalf("nothing says where it was written: %v", result.Rows[0])
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file is not where it said: %v", err)
	}
	if !strings.HasPrefix(string(written), "%PDF-") {
		t.Error("what was written is not a PDF")
	}

	// Filing under a customer or a month is the ordinary use, so a
	// subdirectory has to work.
	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "save",
		Payload:   map[string]interface{}{"number": "INV-2", "filename": "2026/08/INV-2.pdf"},
	}); err != nil {
		t.Fatalf("saving into a subdirectory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "2026", "08", "INV-2.pdf")); err != nil {
		t.Errorf("the document is not in its subdirectory: %v", err)
	}
}

func TestAFilenameThatTriesToLeaveTheDirectory(t *testing.T) {
	// The name is data. Joined straight onto the output directory, a flow
	// carrying `../../etc/cron.d/mycel` wrote there — a connector whose job
	// is writing files from data would write them anywhere the process could.
	template := templateIn(t, `<h1>Invoice</h1>`)
	outputDir := t.TempDir()
	c := New("invoices", &Config{Template: template, OutputDir: outputDir})

	for name, filename := range map[string]string{
		"up and out":       "../escaped.pdf",
		"further up":       "../../../tmp/escaped.pdf",
		"through a subdir": "invoices/../../escaped.pdf",
		"an absolute path": "/tmp/escaped.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := c.Write(context.Background(), &connector.Data{
				Operation: "save",
				Payload:   map[string]interface{}{"filename": filename},
			})

			if err == nil {
				path, _ := result.Rows[0]["file_path"].(string)
				// An absolute path is not refused but confined: it is written
				// inside the output directory, which is the same guarantee.
				base, _ := filepath.Abs(outputDir)
				if !strings.HasPrefix(path, base+string(filepath.Separator)) {
					t.Errorf("a document was written outside the output directory: %s", path)
				}
				return
			}
			if !strings.Contains(err.Error(), "outside") {
				t.Errorf("refused for the wrong reason: %v", err)
			}
		})
	}

	// Nothing escaped, whichever way it was refused.
	parent := filepath.Dir(outputDir)
	if _, err := os.Stat(filepath.Join(parent, "escaped.pdf")); err == nil {
		t.Error("a document was written into the parent directory")
	}
	if _, err := os.Stat("/tmp/escaped.pdf"); err == nil {
		_ = os.Remove("/tmp/escaped.pdf")
		t.Error("a document was written to an absolute path outside the output directory")
	}
}

func TestWhichTemplateIsUsed(t *testing.T) {
	// Three places it can come from, in order: the payload wins so one
	// connector can render an invoice and a packing slip.
	fromConfig := templateIn(t, `<h1>From the connector</h1>`)
	fromPayload := templateIn(t, `<h1>From the payload</h1>`)

	c := New("documents", &Config{Template: fromConfig})

	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"template": fromPayload},
	}); err != nil {
		t.Errorf("a template named in the payload was refused: %v", err)
	}

	// And the flow's target as a last resort.
	bare := New("documents", &Config{})
	if _, err := bare.Write(context.Background(), &connector.Data{
		Target:  fromConfig,
		Payload: map[string]interface{}{},
	}); err != nil {
		t.Errorf("a template named as the target was refused: %v", err)
	}

	// None of the three: said plainly, and naming where to put one.
	err := writeErr(t, bare, &connector.Data{Payload: map[string]interface{}{}})
	if err == nil {
		t.Fatal("a document was generated with no template at all")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Errorf("error = %v", err)
	}
}

func TestATemplateThatIsNotThere(t *testing.T) {
	c := New("invoices", &Config{Template: "/nonexistent/invoice.html"})

	err := writeErr(t, c, &connector.Data{Payload: map[string]interface{}{}})
	if err == nil {
		t.Fatal("a document was generated from a template that does not exist")
	}
	// The path is in the message: a template is a file somebody deployed, and
	// the usual failure is that it was not deployed.
	if !strings.Contains(err.Error(), "/nonexistent/invoice.html") {
		t.Errorf("the error does not name the template: %v", err)
	}
}

func TestATemplateThatDoesNotRender(t *testing.T) {
	// A field the payload does not carry, or a template somebody mistyped.
	broken := templateIn(t, `<h1>{{.number</h1>`)
	c := New("invoices", &Config{Template: broken})

	if err := writeErr(t, c, &connector.Data{Payload: map[string]interface{}{}}); err == nil {
		t.Error("a template that does not parse produced a document anyway")
	}
}

func TestAnOperationNobodyImplements(t *testing.T) {
	template := templateIn(t, `<h1>Invoice</h1>`)
	c := New("invoices", &Config{Template: template})

	err := writeErr(t, c, &connector.Data{
		Operation: "email",
		Payload:   map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("an operation this connector does not have was accepted")
	}
	// The message says what it does have, so nobody has to open the
	// documentation for a typo.
	if !strings.Contains(err.Error(), "generate") || !strings.Contains(err.Error(), "save") {
		t.Errorf("the error does not say what is available: %v", err)
	}
}

func TestTheConnectorAndItsSettings(t *testing.T) {
	built, err := (&Factory{}).Create(context.Background(), &connector.Config{
		Name: "invoices",
		Properties: map[string]interface{}{
			"template":    "/srv/templates/invoice.html",
			"output_dir":  "/srv/invoices",
			"page_size":   "Letter",
			"font":        "Times",
			"margin_left": 25,
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c := built.(*Connector)
	if c.config.PageSize != "Letter" || c.config.Font != "Times" {
		t.Errorf("config = %+v", c.config)
	}
	// A margin written as a whole number in HCL arrives as an integer, and
	// read only as a float it would fall back to the default.
	if c.config.MarginLeft != 25 {
		t.Errorf("left margin = %v", c.config.MarginLeft)
	}
	if c.config.MarginTop == 0 || c.config.MarginRight == 0 {
		t.Errorf("a margin nobody set became zero: %+v", c.config)
	}

	if c.Name() != "invoices" || c.Type() != "pdf" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
	if !(&Factory{}).Supports("pdf", "") || (&Factory{}).Supports("file", "") {
		t.Error("the factory answers for the wrong connector type")
	}
	// Nothing to connect to and nothing to close: a PDF is made in process.
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func writeErr(t *testing.T, c *Connector, data *connector.Data) error {
	t.Helper()
	_, err := c.Write(context.Background(), data)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

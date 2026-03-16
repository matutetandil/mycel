// Package pdf implements a PDF generation connector.
// Uses HTML templates with Go template syntax, rendered to PDF via fpdf (pure Go).
package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/matutetandil/mycel/internal/connector"
)

// Connector generates PDF documents from HTML templates.
type Connector struct {
	name   string
	config *Config
}

// Config holds the PDF connector configuration.
type Config struct {
	// Template is the default HTML template file path.
	// Can be overridden per-request via the "template" payload field.
	Template string

	// OutputDir is the default directory for saving PDFs (optional).
	OutputDir string

	// PageSize is the default page size (A4, Letter, Legal). Default: A4.
	PageSize string

	// Font is the default font family. Default: Helvetica.
	Font string

	// MarginLeft, MarginTop, MarginRight in mm. Defaults: 15, 15, 15.
	MarginLeft  float64
	MarginTop   float64
	MarginRight float64
}

// New creates a new PDF connector.
func New(name string, config *Config) *Connector {
	if config.PageSize == "" {
		config.PageSize = "A4"
	}
	if config.Font == "" {
		config.Font = "Helvetica"
	}
	if config.MarginLeft == 0 {
		config.MarginLeft = 15
	}
	if config.MarginTop == 0 {
		config.MarginTop = 15
	}
	if config.MarginRight == 0 {
		config.MarginRight = 15
	}
	return &Connector{name: name, config: config}
}

func (c *Connector) Name() string                       { return c.name }
func (c *Connector) Type() string                       { return "pdf" }
func (c *Connector) Connect(_ context.Context) error    { return nil }
func (c *Connector) Close(_ context.Context) error      { return nil }
func (c *Connector) Health(_ context.Context) error      { return nil }

// Write generates a PDF.
//
// Operations:
//   - "generate" — returns PDF bytes as base64 in the result (for HTTP responses)
//   - "save"     — saves PDF to file, returns file path
//
// Expected payload fields:
//   - template (string): path to HTML template file
//   - data (map):        template variables
//   - filename (string): output filename (for save operation)
func (c *Connector) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	// Template resolution order: payload override > connector config > target fallback
	templatePath, _ := data.Payload["template"].(string)
	if templatePath == "" {
		templatePath = c.config.Template
	}
	if templatePath == "" {
		templatePath = data.Target
	}
	if templatePath == "" {
		return nil, fmt.Errorf("pdf: template path is required (set 'template' in connector config or payload)")
	}

	// Read template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("pdf: failed to read template %s: %w", templatePath, err)
	}

	// Build template data from payload (exclude meta fields)
	tmplData := make(map[string]interface{})
	for k, v := range data.Payload {
		if k != "template" && k != "filename" {
			tmplData[k] = v
		}
	}

	// Render Go template
	rendered, err := renderTemplate(string(tmplContent), tmplData)
	if err != nil {
		return nil, fmt.Errorf("pdf: template render error: %w", err)
	}

	// Parse HTML and render to PDF
	pdfBytes, err := renderHTMLToPDF(rendered, c.config)
	if err != nil {
		return nil, fmt.Errorf("pdf: render error: %w", err)
	}

	operation := data.Operation
	if operation == "" {
		operation = "generate"
	}

	switch operation {
	case "generate":
		// Return PDF bytes as base64 for HTTP response
		encoded := base64.StdEncoding.EncodeToString(pdfBytes)
		filename, _ := data.Payload["filename"].(string)
		if filename == "" {
			filename = "document.pdf"
		}

		return &connector.Result{
			Rows: []map[string]interface{}{
				{
					"_binary":       encoded,
					"_content_type": "application/pdf",
					"_filename":     filename,
					"size":          len(pdfBytes),
				},
			},
			Affected: 1,
		}, nil

	case "save":
		filename, _ := data.Payload["filename"].(string)
		if filename == "" {
			filename = "document.pdf"
		}

		outputDir := c.config.OutputDir
		if outputDir == "" {
			outputDir = "."
		}

		outPath := filepath.Join(outputDir, filename)

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return nil, fmt.Errorf("pdf: failed to create output directory: %w", err)
		}

		if err := os.WriteFile(outPath, pdfBytes, 0644); err != nil {
			return nil, fmt.Errorf("pdf: failed to write file: %w", err)
		}

		return &connector.Result{
			Rows: []map[string]interface{}{
				{
					"file_path": outPath,
					"filename":  filename,
					"size":      len(pdfBytes),
				},
			},
			Affected: 1,
		}, nil

	default:
		return nil, fmt.Errorf("pdf: unknown operation %q (use 'generate' or 'save')", operation)
	}
}

// GenerateBytes generates a PDF from a template string and data, returning raw bytes.
// Useful for testing.
func (c *Connector) GenerateBytes(templateHTML string, data map[string]interface{}) ([]byte, error) {
	rendered, err := renderTemplate(templateHTML, data)
	if err != nil {
		return nil, err
	}
	return renderHTMLToPDF(rendered, c.config)
}

// renderHTMLToPDF converts rendered HTML to PDF bytes.
func renderHTMLToPDF(html string, config *Config) ([]byte, error) {
	renderer := newRenderer(config)
	if err := renderer.render(html); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := renderer.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output error: %w", err)
	}

	return buf.Bytes(), nil
}

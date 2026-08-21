package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// PDFSchema implements ConnectorSchemaProvider for PDF.
type PDFSchema struct{}

func (PDFSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "template", Doc: "HTML template file path", Type: schema.TypeString, Required: true},
			{Name: "output_dir", Doc: "Output directory for generated PDFs", Type: schema.TypeString},
			{Name: "page_size", Doc: "Page size (e.g., A4, Letter)", Type: schema.TypeString},
			{Name: "font", Doc: "Default font family", Type: schema.TypeString},
			{Name: "margin_left", Doc: "Left margin in mm", Type: schema.TypeNumber},
			{Name: "margin_top", Doc: "Top margin in mm", Type: schema.TypeNumber},
			{Name: "margin_right", Doc: "Right margin in mm", Type: schema.TypeNumber},
		},
	}
}

func (PDFSchema) SourceSchema() *schema.Block { return nil }

func (PDFSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "PDF operation", Type: schema.TypeString, Values: []string{"generate", "save"}},
		},
	}
}

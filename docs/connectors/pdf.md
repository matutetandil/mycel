# PDF

Generate PDF documents from HTML templates. Uses Go's `text/template` syntax for dynamic content and pure Go rendering (no external binaries required).

## Configuration

```hcl
connector "pdf" {
  type         = "pdf"
  page_size    = "A4"
  font         = "Helvetica"
  margin_left  = 15
  margin_top   = 15
  margin_right = 15
  output_dir   = "./pdfs"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `page_size` | string | `"A4"` | Page size: `A4`, `Letter`, `Legal` |
| `font` | string | `"Helvetica"` | Default font family |
| `margin_left` | number | `15` | Left margin in mm |
| `margin_top` | number | `15` | Top margin in mm |
| `margin_right` | number | `15` | Right margin in mm |
| `output_dir` | string | `"."` | Default output directory for `save` operation |

## Operations

| Operation | Description | Output |
|-----------|-------------|--------|
| `generate` | Returns PDF as base64 binary (for HTTP responses) | `_binary`, `_content_type`, `_filename`, `size` |
| `save` | Saves PDF to file system | `file_path`, `filename`, `size` |

### generate (default)

Returns the PDF as a binary HTTP response. The REST connector automatically detects the `_binary` and `_content_type` fields and serves the raw PDF with appropriate headers (`Content-Type: application/pdf`, `Content-Disposition: attachment`).

```hcl
flow "download_invoice" {
  from {
    connector = "api"
    operation = "GET /invoices/:id/pdf"
  }

  step "invoice" {
    connector = "db"
    query     = "SELECT * FROM invoices WHERE id = ?"
    params    = [input.params.id]
  }

  transform {
    template = "'./templates/invoice.html'"
    filename = "'invoice-' + step.invoice.number + '.pdf'"
    number   = "step.invoice.number"
    date     = "step.invoice.date"
    total    = "step.invoice.total"
    customer = "step.invoice.customer_name"
  }

  to {
    connector = "pdf"
    operation = "generate"
  }
}
```

**Response:** The browser receives a PDF file download directly.

### save

Writes the PDF to the filesystem. The file is saved to `output_dir/filename`.

```hcl
flow "archive_report" {
  from {
    connector = "api"
    operation = "POST /reports/generate"
  }

  transform {
    template = "'./templates/report.html'"
    filename = "'report-' + now() + '.pdf'"
    title    = "input.title"
    data     = "input.data"
  }

  to {
    connector = "pdf"
    operation = "save"
  }
}
```

**Response:**
```json
{"file_path": "./pdfs/report-2026-03-16.pdf", "filename": "report-2026-03-16.pdf", "size": 24576}
```

## Payload Fields

The `transform` block builds the payload sent to the PDF connector:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `template` | string | Yes | Path to the HTML template file |
| `filename` | string | No | Output filename (default: `document.pdf`) |
| *(any other)* | any | No | Template variables available as `{{.field_name}}` |

All fields except `template` and `filename` are passed as template data.

## HTML Templates

Templates use [Go template syntax](https://pkg.go.dev/text/template) with HTML markup. The template is rendered first (variable substitution, loops, conditionals), then the resulting HTML is converted to PDF.

### Supported HTML Elements

| Element | Rendering |
|---------|-----------|
| `h1` - `h6` | Headings with decreasing font sizes (24px - 10px) |
| `p` | Paragraphs with spacing |
| `strong` / `b` | Bold text |
| `em` / `i` | Italic text |
| `table`, `tr`, `th`, `td` | Tables with borders and header highlighting |
| `ul` / `ol`, `li` | Bulleted and numbered lists |
| `hr` | Horizontal rule |
| `br` | Line break |
| `img` | Images (local files only) |
| `div` | Container (inherits styles) |

### Supported Inline CSS

Apply styles via the `style` attribute on any element:

| Property | Values | Example |
|----------|--------|---------|
| `text-align` | `left`, `center`, `right` | `style="text-align: right"` |
| `font-size` | Pixel values | `style="font-size: 18px"` |
| `color` | Hex colors | `style="color: #333333"` |
| `background-color` | Hex colors | `style="background-color: #f0f0f0"` |

### Template Syntax

```html
<!-- Variable substitution -->
<h1>Invoice #{{.number}}</h1>
<p>Date: {{.date}}</p>
<p>Customer: {{.customer}}</p>

<!-- Conditionals -->
{{if .discount}}
<p style="color: #cc0000">Discount: {{.discount}}%</p>
{{end}}

<!-- Loops -->
<table>
  <tr><th>Item</th><th>Qty</th><th>Price</th><th>Total</th></tr>
  {{range .items}}
  <tr>
    <td>{{.name}}</td>
    <td>{{.quantity}}</td>
    <td>${{.price}}</td>
    <td>${{.line_total}}</td>
  </tr>
  {{end}}
</table>

<hr>
<p style="text-align: right"><strong>Total: ${{.total}}</strong></p>
```

## Complete Example

### Invoice PDF Generation

**Connector:**
```hcl
connector "api" {
  type   = "rest"
  driver = "server"
  port   = 3000
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

connector "pdf" {
  type      = "pdf"
  page_size = "A4"
  font      = "Helvetica"
}
```

**Flow:**
```hcl
flow "get_invoice_pdf" {
  from {
    connector = "api"
    operation = "GET /invoices/:id/pdf"
  }

  step "invoice" {
    connector = "db"
    query     = "SELECT * FROM invoices WHERE id = ?"
    params    = [input.params.id]
  }

  step "items" {
    connector = "db"
    query     = "SELECT * FROM invoice_items WHERE invoice_id = ?"
    params    = [input.params.id]
  }

  transform {
    template = "'./templates/invoice.html'"
    filename = "'invoice-' + step.invoice.number + '.pdf'"
    number   = "step.invoice.number"
    date     = "step.invoice.date"
    customer = "step.invoice.customer_name"
    items    = "step.items"
    total    = "step.invoice.total"
  }

  to {
    connector = "pdf"
    operation = "generate"
  }
}
```

**Template (`templates/invoice.html`):**
```html
<h1 style="color: #2c3e50">Invoice #{{.number}}</h1>
<hr>
<p><strong>Date:</strong> {{.date}}</p>
<p><strong>Customer:</strong> {{.customer}}</p>

<table>
  <tr>
    <th>Item</th>
    <th>Quantity</th>
    <th>Price</th>
    <th>Total</th>
  </tr>
  {{range .items}}
  <tr>
    <td>{{.name}}</td>
    <td>{{.quantity}}</td>
    <td>${{.price}}</td>
    <td>${{.line_total}}</td>
  </tr>
  {{end}}
</table>

<hr>
<p style="text-align: right; font-size: 18px">
  <strong>Total: ${{.total}}</strong>
</p>
```

**Usage:**
```bash
curl http://localhost:3000/invoices/42/pdf --output invoice.pdf
```

## Binary HTTP Responses

When using the `generate` operation, the PDF connector returns special fields that the REST connector recognizes:

- `_binary` — Base64-encoded PDF content
- `_content_type` — `application/pdf`
- `_filename` — Suggested filename for `Content-Disposition` header

This mechanism works for any binary content, not just PDFs. Any flow that returns `_binary` + `_content_type` will be served as a binary download by the REST connector.

---

> **Full configuration reference:** See [PDF](../reference/configuration.md#pdf) in the Configuration Reference.

# PDF

Rendering a document a person will read: an invoice, from a row in a database
and an HTML template, with no headless browser and no external binary.

The same connector does two jobs, and which one is the flow's `operation`:
`generate` hands the bytes back to the caller, `save` writes a file.

## Files

| File | Purpose |
|------|---------|
| `connectors.mycel` | REST, SQLite, and the pdf connector |
| `flows.mycel` | Download one, archive one, list them |
| `templates/invoice.html` | The document, in Go template syntax |
| `migrations/001_invoices.sql` | The table and two invoices |

## Running

```bash
mycel migrate --config ./examples/pdf
mycel start --config ./examples/pdf
```

## Try It

The invoices to pick from:

```bash
curl http://localhost:3000/invoices
```

Download one. What comes back is a PDF, not JSON describing one — the REST
connector recognises the bytes the pdf connector produced and answers with
`Content-Type: application/pdf` and a `Content-Disposition` filename:

```bash
curl -o invoice.pdf http://localhost:3000/invoices/1/pdf
```

```bash
file invoice.pdf
# invoice.pdf: PDF document, version 1.3, 1 pages
```

Archive the other one instead of sending it. `save` writes into the
connector's `output_dir` and answers with where the file went:

```bash
curl -X POST http://localhost:3000/invoices/2/archive
```

```json
{
  "file_path": "./out/invoice-INV-002.pdf",
  "filename": "invoice-INV-002.pdf",
  "size": 1769
}
```

## How the template gets its data

Everything the `transform` block produces becomes a template variable, except
two names the connector keeps for itself:

| Field | Meaning |
|-------|---------|
| `filename` | What the file is called — the download name, or the name on disk |
| `template` | A template path for this one request, overriding the connector's |
| anything else | A variable: `number` is `{{.number}}` |

Which is why the flows shape the row before writing it, and why `total` is
turned into a string: a template writes what it is given.

The template is Go's [`text/template`](https://pkg.go.dev/text/template) over
HTML — `{{.number}}` substitutes, `{{range}}` loops, `{{if}}` decides. The
result is rendered to PDF directly, so the HTML it understands is a practical
subset: headings, paragraphs, `strong`/`em`, tables, lists, `hr`, `img`, and
inline `style` attributes. [The connector page](../../docs/connectors/pdf.md)
lists it in full.

## Notes

`output_dir` is where `save` writes, and the filename comes from the payload —
so it is worth keeping the filename out of a caller's hands, or building it
from something you control, as these flows do with the invoice number.

There is no browser here: rendering is pure Go, which is why this runs in a
container with nothing else installed in it.

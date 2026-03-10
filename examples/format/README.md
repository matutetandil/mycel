# Format Declarations Example

Demonstrates Mycel's multi-format I/O system. The same service handles JSON and XML at different levels.

## What This Example Does

- Exposes JSON CRUD endpoints at `/products`
- Exposes XML endpoints at `/products.xml`
- Shows a mixed-format flow: JSON in, XML enrichment via SOAP, JSON out
- Demonstrates format at connector, flow, and step levels

## Quick Start

```bash
mycel start --config ./examples/format
```

## Format Levels

### 1. Connector Level

Sets the default format for all operations on that connector:

```hcl
connector "soap_api" {
  type     = "http"
  base_url = "https://legacy.example.com"
  format   = "xml"
}
```

### 2. Flow Level

Overrides the connector default for a specific flow:

```hcl
flow "get_products_xml" {
  from {
    connector = "api"
    operation = "GET /products.xml"
    format    = "xml"       # This endpoint accepts/returns XML
  }
  to {
    connector = "sqlite"
    target    = "products"
  }
}
```

### 3. Step Level

Sets format for an individual step within a multi-step flow:

```hcl
step "get_legacy_info" {
  connector = "soap_api"
  operation = "GET /products/${step.save.id}/details"
  format    = "xml"         # This step communicates in XML
}
```

### 4. Auto-Detection

When no format is declared, Mycel infers it from the `Content-Type` header:
- `application/json` -> JSON
- `application/xml` or `text/xml` -> XML

**Resolution priority:** Step > Flow > Content-Type header > Connector > Default (JSON)

## Try It

### JSON (default)

```bash
# Create product (JSON)
curl -X POST http://localhost:3000/products \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 19.99}'

# List products (JSON)
curl http://localhost:3000/products
```

### XML (flow-level format)

```bash
# Create product (XML)
curl -X POST http://localhost:3000/products.xml \
  -H "Content-Type: application/xml" \
  -d '<product><name>Widget</name><price>19.99</price></product>'

# List products (XML)
curl http://localhost:3000/products.xml
```

Expected XML response:

```xml
<products>
  <product>
    <id>1</id>
    <name>Widget</name>
    <price>19.99</price>
  </product>
</products>
```

### Auto-Detection (Content-Type header)

```bash
# Send XML to a JSON endpoint - Mycel detects format from Content-Type
curl -X POST http://localhost:3000/products \
  -H "Content-Type: application/xml" \
  -d '<product><name>Gadget</name><price>29.99</price></product>'
```

### Mixed Format Flow

```bash
# JSON in -> enrichment calls SOAP (XML) -> JSON out
curl -X POST http://localhost:3000/products/enrich \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 19.99}'
```

Expected response (JSON, enriched with legacy XML data):

```json
{
  "id": 1,
  "name": "Widget",
  "price": 19.99,
  "legacy_code": "WDG-001",
  "warehouse": "US-EAST-1"
}
```

## File Structure

```
format/
├── config.hcl              # Service name and version
├── connectors/
│   ├── api.hcl             # REST API (default JSON)
│   ├── database.hcl        # SQLite database
│   └── soap_api.hcl        # External SOAP client (format = "xml")
├── flows/
│   ├── json_crud.hcl       # Standard JSON CRUD
│   ├── xml_endpoint.hcl    # XML endpoints (flow-level format)
│   └── mixed_format.hcl    # JSON + XML in one flow (step-level format)
└── README.md
```

## Learn More

- [Format System Reference](../../docs/FORMAT.md) - XML mapping rules, codec extensibility
- [SOAP Connector](../../docs/connectors/soap.md) - Full SOAP 1.1/1.2 support
- [Basic Example](../basic) - Simple REST + SQLite without format declarations

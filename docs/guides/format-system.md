# Format System

Mycel uses `map[string]interface{}` as its internal data representation. The format system handles serialization and deserialization at protocol boundaries: incoming requests, outgoing responses, and calls to external APIs.

---

## Supported Formats

| Format | Content-Type | Implementation |
|--------|-------------|----------------|
| `json` | `application/json` | `encoding/json` (stdlib) |
| `xml` | `application/xml` | `encoding/xml` (stdlib) |

The default format is `json` unless overridden.

---

## Configuration

### On Connectors

Setting `format` on a connector applies it as the default for all operations on that connector.

```hcl
connector "legacy_api" {
  type     = "http"
  base_url = "https://legacy.example.com"
  format   = "xml"
}

connector "xml_server" {
  type   = "rest"
  port   = 3000
  format = "xml"
}
```

### On Flows

Setting `format` on a `from` or `to` block overrides the connector-level default for that specific flow.

```hcl
flow "bridge" {
  from {
    connector = "api"
    operation = "POST /convert"
    format    = "xml"
  }

  to {
    connector = "modern_api"
    format    = "json"
  }
}
```

### On Steps

Individual steps can also specify a format independently.

```hcl
step "get_legacy" {
  connector = "legacy_api"
  operation = "GET /data"
  format    = "xml"
}
```

---

## Auto-Detection

Mycel attempts to detect the correct format automatically when it is not explicitly configured.

- **REST server (incoming requests)**: format is inferred from the `Content-Type` request header
- **HTTP client (responses)**: format is inferred from the `Content-Type` response header

**Resolution priority (highest to lowest):**

1. Format explicitly set on the flow or step
2. Format detected from `Content-Type` header
3. Format set on the connector
4. Default: `json`

---

## XML Mapping Rules

When XML is decoded, it is converted to a `map[string]interface{}` that Mycel can process normally. The following rules apply:

| XML construct | Map representation |
|--------------|-------------------|
| Element name | map key |
| Text content | string value |
| Child elements | nested map |
| Repeated elements | `[]interface{}` slice |
| Attributes | keys prefixed with `@` |
| Text content alongside attributes | `#text` key |

### Example

```xml
<product id="42" category="widgets">
  <name>Widget</name>
  <price currency="USD">19.99</price>
  <tag>sale</tag>
  <tag>new</tag>
</product>
```

Decoded to:

```json
{
  "@id": "42",
  "@category": "widgets",
  "name": "Widget",
  "price": {
    "@currency": "USD",
    "#text": "19.99"
  },
  "tag": ["sale", "new"]
}
```

The resulting map is available in transforms and step references as normal:

```hcl
transform {
  output.product_id   = input.product["@id"]
  output.product_name = input.product.name
  output.currency     = input.product.price["@currency"]
  output.price        = input.product.price["#text"]
}
```

---

## Extensibility

Custom codecs can be registered at startup to add support for additional formats.

```go
codec.Register("yaml", &YAMLCodec{})
```

A codec must implement the following interface:

```go
type Codec interface {
    Encode(v interface{}) ([]byte, error)
    Decode(data []byte, v interface{}) error
    ContentType() string
    Name() string
}
```

Once registered, the custom format name can be used in any `format` field in HCL configuration.

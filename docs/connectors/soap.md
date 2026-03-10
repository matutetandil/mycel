# SOAP Connector

The SOAP connector enables bidirectional integration with SOAP/XML web services. It operates in two modes detected automatically from configuration:

- **Client mode** (when `endpoint` is set): calls external SOAP services
- **Server mode** (when `port` is set): exposes operations as a SOAP endpoint

```hcl
connector "name" {
  type = "soap"
  # client: endpoint = "..."
  # server: port     = 8081
}
```

---

## Client Mode

Calls external SOAP services. Implements `Reader` and `Writer`.

### Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| endpoint | string | yes | - | SOAP service URL |
| soap_version | string | no | `"1.1"` | Protocol version: `"1.1"` or `"1.2"` |
| namespace | string | no | - | Service XML namespace |
| timeout | string | no | `"30s"` | Request timeout |
| auth.type | string | no | - | Authentication type: `"basic"` or `"bearer"` |
| auth.username | string | no | - | Basic auth username |
| auth.password | string | no | - | Basic auth password |
| auth.token | string | no | - | Bearer token |

### Example

```hcl
connector "erp" {
  type         = "soap"
  endpoint     = "https://erp.example.com/service"
  soap_version = "1.1"
  namespace    = "http://example.com/service"
  timeout      = "30s"

  auth {
    type     = "basic"
    username = env("SOAP_USER")
    password = env("SOAP_PASS")
  }
}
```

### Flow Usage

The `operation` field maps to the SOAP operation name (used as the SOAPAction and envelope body element).

```hcl
flow "get_order" {
  from {
    connector = "api"
    operation = "GET /orders/{id}"
  }

  transform {
    output.OrderID = input.id
  }

  to {
    connector = "erp"
    operation = "GetOrder"
  }
}
```

---

## Server Mode

Exposes SOAP operations as an HTTP endpoint. Implements `RouteRegistrar` and `Starter`.

### Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| port | number | yes | - | HTTP port to listen on |
| soap_version | string | no | `"1.1"` | Protocol version: `"1.1"` or `"1.2"` |
| namespace | string | no | - | Service XML namespace |

### Example

```hcl
connector "soap_server" {
  type         = "soap"
  port         = 8081
  soap_version = "1.1"
  namespace    = "http://myservice.example.com"
}
```

### Flow Usage

The `operation` field declares which SOAP operation this flow handles.

```hcl
flow "handle_create_order" {
  from {
    connector = "soap_server"
    operation = "CreateOrder"
  }

  transform {
    output.id    = uuid()
    output.name  = input.OrderName
    output.total = input.OrderTotal
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
```

---

## Protocol Details

### SOAP 1.1

- Uses `SOAPAction` HTTP header to indicate the operation
- Content-Type: `text/xml; charset=utf-8`

### SOAP 1.2

- Operation indicated via `action` parameter in Content-Type
- Content-Type: `application/soap+xml; charset=utf-8; action="..."`

### WSDL

In server mode, Mycel auto-generates a WSDL document describing all registered operations, available at:

```
GET /wsdl
```

### Error Handling

SOAP faults are automatically mapped to Mycel errors and propagated through the normal `on_error` handling. In server mode, errors in flow execution are returned as well-formed SOAP fault responses.

---

## Bridging Patterns

The SOAP connector is commonly used to bridge between protocol worlds without writing any code.

**SOAP to REST**

```hcl
flow "soap_to_rest" {
  from { connector = "soap_server", operation = "PlaceOrder" }
  to   { connector = "modern_api",  operation = "POST /orders" }
}
```

**REST to SOAP**

```hcl
flow "rest_to_soap" {
  from { connector = "api", operation = "POST /orders" }
  to   { connector = "erp", operation = "CreateOrder" }
}
```

**SOAP to Database**

```hcl
flow "soap_to_db" {
  from { connector = "soap_server", operation = "SubmitRecord" }
  to   { connector = "db",          target    = "records" }
}
```

**SOAP to Queue**

```hcl
flow "soap_to_queue" {
  from { connector = "soap_server", operation = "PublishEvent" }
  to   { connector = "rabbitmq",    target    = "events" }
}
```

---

> **Full configuration reference:** See [SOAP](../reference/configuration.md#soap) in the Configuration Reference.

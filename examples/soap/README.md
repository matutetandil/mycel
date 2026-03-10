# SOAP Example

A SOAP web service backed by SQLite, demonstrating Mycel's SOAP connector in server mode.

## What This Example Does

- Exposes a SOAP endpoint on port 8080
- Auto-generates a WSDL document at `/wsdl`
- Implements `GetUser` and `CreateUser` operations
- Stores data in a local SQLite database
- Validates input with type schemas

## Quick Start

```bash
# From the repository root
mycel start --config ./examples/soap

# Or with Docker
docker run -v $(pwd)/examples/soap:/etc/mycel -p 8080:8080 ghcr.io/matutetandil/mycel
```

## Verify It Works

### 1. Fetch the WSDL

```bash
curl http://localhost:8080/wsdl
```

Returns the auto-generated WSDL describing all registered operations.

### 2. Create a user (SOAP 1.1)

```bash
curl -X POST http://localhost:8080/ws \
  -H 'Content-Type: text/xml; charset=utf-8' \
  -H 'SOAPAction: "CreateUser"' \
  -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CreateUser xmlns="http://example.com/users">
      <Email>john@example.com</Email>
      <Name>John Doe</Name>
    </CreateUser>
  </soap:Body>
</soap:Envelope>'
```

Expected response:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CreateUserResponse xmlns="http://example.com/users">
      <id>1</id>
      <email>john@example.com</email>
      <name>John Doe</name>
    </CreateUserResponse>
  </soap:Body>
</soap:Envelope>
```

### 3. Get a user (SOAP 1.1)

```bash
curl -X POST http://localhost:8080/ws \
  -H 'Content-Type: text/xml; charset=utf-8' \
  -H 'SOAPAction: "GetUser"' \
  -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser xmlns="http://example.com/users">
      <Email>john@example.com</Email>
    </GetUser>
  </soap:Body>
</soap:Envelope>'
```

### 4. Using SOAP 1.2

For SOAP 1.2, change the connector's `soap_version` to `"1.2"` and use the corresponding Content-Type:

```bash
curl -X POST http://localhost:8080/ws \
  -H 'Content-Type: application/soap+xml; charset=utf-8; action="CreateUser"' \
  -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope">
  <soap:Body>
    <CreateUser xmlns="http://example.com/users">
      <Email>jane@example.com</Email>
      <Name>Jane Doe</Name>
    </CreateUser>
  </soap:Body>
</soap:Envelope>'
```

Note the differences from SOAP 1.1:
- Content-Type is `application/soap+xml` (not `text/xml`)
- The operation is specified via the `action` parameter in Content-Type (no `SOAPAction` header)
- The envelope namespace is `http://www.w3.org/2003/05/soap-envelope`

## File Structure

```
soap/
├── config.hcl              # Service name and version
├── connectors/
│   ├── soap_server.hcl     # SOAP endpoint configuration
│   └── database.hcl        # SQLite database connection
├── flows/
│   ├── get_user.hcl        # GetUser operation
│   └── create_user.hcl     # CreateUser operation
├── types/
│   └── user.hcl            # User input validation schema
├── data/
│   └── app.db              # SQLite database file (created automatically)
└── setup.sql               # Initial database schema
```

## Next Steps

- Bridge SOAP to REST: Add a REST connector and forward SOAP operations to REST endpoints
- Add authentication: Use `auth { type = "basic" }` on the connector
- Switch to PostgreSQL: Change the database connector driver to `"postgres"`
- See the [SOAP connector reference](../../docs/connectors/soap.md) for all options

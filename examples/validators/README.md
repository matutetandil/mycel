# Custom Validators Example

This example demonstrates how to create and use custom validators in Mycel using regex patterns and CEL expressions.

## Overview

Validators provide reusable validation logic that can be applied to type fields. Mycel supports three types of validators:

| Type | Use Case | Example |
|------|----------|---------|
| `regex` | Pattern matching | Email format, phone numbers, UUIDs |
| `cel` | Expression-based logic | Age ranges, business rules, complex conditions |
| `wasm` | Custom compiled code | Legacy validation, external libraries |

## Regex Validators

Simple pattern matching using regular expressions:

```hcl
validator "email" {
  type    = "regex"
  pattern = "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
  message = "Invalid email format"
}

validator "phone_ar" {
  type    = "regex"
  pattern = "^\\+54[0-9]{10,11}$"
  message = "Invalid Argentine phone number"
}

validator "uuid" {
  type    = "regex"
  pattern = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-..."
  message = "Invalid UUID format"
}
```

## CEL Validators

Expression-based validation using [CEL (Common Expression Language)](https://github.com/google/cel-spec):

```hcl
validator "adult_age" {
  type    = "cel"
  expr    = "value >= 18 && value <= 120"
  message = "Age must be between 18 and 120"
}

validator "valid_status" {
  type    = "cel"
  expr    = "value in ['pending', 'active', 'suspended', 'cancelled']"
  message = "Invalid status"
}

validator "strong_password" {
  type = "cel"
  expr = <<-CEL
    size(value) >= 8 &&
    value.matches("[A-Z]") &&
    value.matches("[a-z]") &&
    value.matches("[0-9]") &&
    value.matches("[!@#$%^&*]")
  CEL
  message = "Password must have 8+ chars with uppercase, lowercase, number, and special char"
}
```

### CEL Functions Available

| Function | Description | Example |
|----------|-------------|---------|
| `size(s)` | String length | `size(value) >= 8` |
| `matches(pattern)` | Regex match | `value.matches("[A-Z]")` |
| `startsWith(s)` | Prefix check | `value.startsWith("https://")` |
| `endsWith(s)` | Suffix check | `value.endsWith(".com")` |
| `contains(s)` | Substring check | `value.contains("@")` |
| `in` | List membership | `value in ['a', 'b', 'c']` |
| `timestamp(s)` | Parse timestamp | `timestamp(value) > now()` |
| `now()` | Current time | `timestamp(value) > now()` |

## Using Validators in Types

Reference validators in type definitions:

```hcl
type "user" {
  email    = string { validate = "validator.email" }
  password = string { validate = "validator.strong_password" }
  age      = number { validate = "validator.adult_age" }
  phone    = string { validate = "validator.phone_ar" }
  status   = string { validate = "validator.valid_status" }
}
```

## Validation in Flows

Validators are applied automatically when types are used:

```hcl
flow "create_user" {
  from {
    connector.api = "POST /users"
  }

  input {
    type = "user"  # Validators run here
  }

  to {
    connector.db = "INSERT INTO users ..."
  }
}
```

If validation fails, the flow returns a 400 error with the validator's message.

## Example Validators in This Demo

### Regex-based

| Validator | Pattern | Purpose |
|-----------|---------|---------|
| `email` | Email format | Standard email validation |
| `phone_ar` | +54 + 10-11 digits | Argentine phone numbers |
| `uuid` | UUID v4 format | Unique identifiers |
| `slug` | lowercase-with-dashes | URL-friendly strings |
| `username` | Letter start, 3-20 chars | User account names |

### CEL-based

| Validator | Expression | Purpose |
|-----------|------------|---------|
| `adult_age` | 18-120 range | Age verification |
| `positive_number` | > 0 | Quantity validation |
| `valid_status` | Enum list | Status field validation |
| `strong_password` | Complex rules | Password strength |
| `future_date` | > now() | Future dates only |
| `reasonable_price` | 0.01 - 999999.99 | Price range |

## Running

```bash
# Start the service
mycel start --config ./examples/validators

# Test email validation
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email": "invalid-email"}'
# Returns: 400 Bad Request - Invalid email format

# Test with valid data
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "age": 25}'
# Returns: 201 Created
```

## WASM Validators

For complex validation logic, use compiled WASM modules:

```hcl
validator "argentina_cuit" {
  type       = "wasm"
  wasm       = "./validators/cuit.wasm"
  entrypoint = "validate"
  message    = "Invalid CUIT number"
}
```

See the `wasm-validator` example for implementation details.

## Best Practices

1. **Reuse validators**: Define once, use in multiple types
2. **Clear messages**: Include valid format in error messages
3. **Combine validators**: Use multiple validators per field when needed
4. **CEL for business logic**: Complex rules belong in CEL, not regex
5. **WASM for performance**: Use WASM for computationally expensive validation

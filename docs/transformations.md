# Transformations in Mycel

Mycel uses **Google's Common Expression Language (CEL)** for data transformations. CEL is a non-Turing complete language designed for safe, fast, and portable expression evaluation.

## Why CEL?

- **Safe**: Sandboxed execution, no side effects, no loops that could hang
- **Fast**: Expressions are compiled once and cached for reuse
- **Validated at startup**: Syntax and type errors are caught when loading configuration, not at runtime
- **Portable**: Same expressions work everywhere (used by Kubernetes, Firebase, Google Cloud IAM)
- **Full-featured**: Supports complex logic, list operations, and custom functions

## Basic Syntax

### Transform Block in Flows

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    # Each line: target_field = "CEL expression"
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    name       = "input.name"
    created_at = "now()"
    status     = "input.age >= 18 ? 'active' : 'pending'"
  }

  to {
    connector = "database"
    target    = "users"
  }
}
```

### Available Variables

| Variable | Description |
|----------|-------------|
| `input` | The incoming request data (map) |
| `output` | Already-set output fields (for referencing previous transforms) |
| `ctx` | Request context (headers, path params, etc.) |
| `enriched` | Data fetched from external services via `enrich` blocks |

## Operators

### Arithmetic
```cel
input.price * input.quantity        // Multiplication
input.total / 100                   // Division
input.count + 1                     // Addition
input.balance - input.withdrawal   // Subtraction
input.value % 2                     // Modulo
```

### Comparison
```cel
input.age >= 18                     // Greater than or equal
input.status == "active"            // Equality
input.role != "admin"               // Inequality
input.score > 90                    // Greater than
input.price < 100                   // Less than
input.level <= 5                    // Less than or equal
```

### Logical
```cel
input.active && input.verified      // AND
input.admin || input.moderator      // OR
!input.deleted                      // NOT
```

### Ternary (Conditional)
```cel
input.age >= 18 ? "adult" : "minor"
input.score >= 90 ? "A" : input.score >= 80 ? "B" : "C"
```

### String Concatenation
```cel
input.first_name + " " + input.last_name
"Hello, " + input.name + "!"
```

## Custom Mycel Functions

These functions are specific to Mycel and always available:

### ID Generation

| Function | Description | Example |
|----------|-------------|---------|
| `uuid()` | Generate UUID v4 | `"550e8400-e29b-41d4-a716-446655440000"` |

```cel
uuid()  // Returns: "550e8400-e29b-41d4-a716-446655440000"
```

### Timestamps

| Function | Description | Example Output |
|----------|-------------|----------------|
| `now()` | Current time in RFC3339 | `"2025-12-29T15:04:05Z"` |
| `now_unix()` | Current Unix timestamp | `1735488245` |

```cel
now()       // Returns: "2025-12-29T15:04:05Z"
now_unix()  // Returns: 1735488245
```

### String Manipulation

| Function | Description | Example |
|----------|-------------|---------|
| `lower(s)` | Convert to lowercase | `lower("HELLO")` → `"hello"` |
| `upper(s)` | Convert to uppercase | `upper("hello")` → `"HELLO"` |
| `trim(s)` | Remove leading/trailing whitespace | `trim("  hi  ")` → `"hi"` |
| `replace(s, old, new)` | Replace all occurrences | `replace("hello", "l", "L")` → `"heLLo"` |
| `substring(s, start, end)` | Extract substring | `substring("hello", 1, 4)` → `"ell"` |
| `split(s, sep)` | Split string into list | `split("a,b,c", ",")` → `["a", "b", "c"]` |
| `join(list, sep)` | Join list into string | `join(["a", "b"], "-")` → `"a-b"` |
| `len(s)` | String length | `len("hello")` → `5` |

```cel
// Chained operations
lower(trim(input.email))

// Replace and transform
replace(lower(input.name), " ", "_")

// Extract domain from email
split(input.email, "@")[1]
```

### Default Values

| Function | Description |
|----------|-------------|
| `default(value, fallback)` | Return fallback if value is null or empty string |
| `coalesce(value, fallback)` | Same as default (alias) |

```cel
default(input.nickname, input.name)     // Use name if nickname is empty
coalesce(input.phone, "N/A")            // "N/A" if phone is null/empty
```

### Hashing

| Function | Description |
|----------|-------------|
| `hash_sha256(s)` | SHA256 hash of string (hex encoded) |

```cel
hash_sha256(input.password)  // Returns hex-encoded hash
```

### Date Formatting

| Function | Description |
|----------|-------------|
| `format_date(date, format)` | Reformat an ISO date |

Format tokens: `YYYY`, `MM`, `DD`, `HH`, `mm`, `ss`

```cel
format_date(input.created_at, "YYYY-MM-DD")  // "2025-12-29"
format_date(now(), "DD/MM/YYYY")             // "29/12/2025"
```

## CEL Standard Library

All CEL built-in functions and macros are available:

### Type Conversions
```cel
int(input.string_number)      // "42" → 42
double(input.integer)         // 42 → 42.0
string(input.number)          // 42 → "42"
```

### String Methods (Built-in)
```cel
input.name.startsWith("Dr.")           // true/false
input.email.endsWith("@gmail.com")     // true/false
input.text.contains("keyword")         // true/false
input.code.matches("[A-Z]{3}[0-9]+")   // regex match
input.name.size()                      // length
```

### List Operations
```cel
input.items.size()                     // List length
input.items[0]                         // First element
input.items[input.items.size() - 1]   // Last element
"admin" in input.roles                 // Contains check

// Macros for list processing
input.items.exists(x, x > 10)          // Any item > 10?
input.items.all(x, x > 0)              // All items > 0?
input.items.filter(x, x > 5)           // Keep items > 5
input.items.map(x, x * 2)              // Double all items
```

## CEL Standard Extensions

Mycel enables all CEL standard extensions for maximum flexibility:

### ext.Strings()

Extended string operations:

```cel
"hello".charAt(1)                    // "e"
"hello world".indexOf("o")           // 4
"hello world".lastIndexOf("o")       // 7
"hello".upperAscii()                 // "HELLO"
"HELLO".lowerAscii()                 // "hello"
"hello".replace("l", "L")            // "heLLo" (first only)
"hello".replace("l", "L", 2)         // "heLLo" (max 2)
"a,b,c".split(",")                   // ["a", "b", "c"]
"hello".substring(1, 4)              // "ell"
"  hello  ".trim()                   // "hello"
"hello".reverse()                    // "olleh"
["a", "b", "c"].join("-")            // "a-b-c"
```

### ext.Encoders()

Base64 encoding/decoding:

```cel
base64.encode(b"hello")              // "aGVsbG8="
base64.decode("aGVsbG8=")            // b"hello"
```

### ext.Math()

Mathematical functions:

```cel
math.abs(-5)                         // 5
math.ceil(4.2)                       // 5
math.floor(4.8)                      // 4
math.round(4.5)                      // 5
math.sign(-10)                       // -1
math.sign(10)                        // 1
math.greatest(1, 5, 3)               // 5
math.least(1, 5, 3)                  // 1
math.isNaN(0.0/0.0)                  // true
math.isInf(1.0/0.0)                  // true
```

### ext.Lists()

Extended list operations:

```cel
[1, 2, 3, 4, 5].slice(1, 4)          // [2, 3, 4]
[[1, 2], [3, 4]].flatten()           // [1, 2, 3, 4]
lists.range(5)                        // [0, 1, 2, 3, 4]
```

### ext.Sets()

Set operations on lists:

```cel
sets.contains([1, 2, 3], [2, 3])     // true (subset check)
sets.equivalent([1, 2], [2, 1])      // true (same elements)
sets.intersects([1, 2], [2, 3])      // true (any common element)
```

## Named/Reusable Transforms

Define transforms once, use in multiple flows:

```hcl
# transforms/user_transforms.hcl
transform "normalize_user" {
  email      = "lower(trim(input.email))"
  name       = "trim(input.name)"
  created_at = "now()"
  updated_at = "now()"
}

transform "audit_fields" {
  created_by = "ctx.user_id"
  created_at = "now()"
}
```

Use in flows:

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    use = "transform.normalize_user"
    # Add or override fields
    id = "uuid()"
  }

  to {
    connector = "database"
    target    = "users"
  }
}
```

## Data Enrichment from External Services

The `enrich` block allows you to fetch data from other microservices (via TCP, HTTP, database, etc.) and use it in your transformations. This is essential for building distributed systems where data lives across multiple services.

### Flow-Level Enrich

Add enrichments directly in a flow for specific use cases:

```hcl
flow "get_product_with_price" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  # Fetch price from pricing microservice
  enrich "pricing" {
    connector = "pricing_service"    # TCP, HTTP, or any connector
    operation = "getPrice"           # Operation/endpoint to call
    params {
      product_id = "input.id"        # CEL expression for params
    }
  }

  # Fetch inventory from stock service
  enrich "inventory" {
    connector = "inventory_api"
    operation = "GET /stock"
    params {
      sku = "input.sku"
    }
  }

  # Use enriched data in transform
  transform {
    id       = "input.id"
    name     = "input.name"
    price    = "enriched.pricing.price"         # Access enriched data
    currency = "enriched.pricing.currency"
    stock    = "enriched.inventory.available"
    in_stock = "enriched.inventory.available > 0"

    # Combine input with enriched data
    total_value = "double(enriched.inventory.available) * enriched.pricing.price"
  }

  to {
    connector = "database"
    target    = "products"
  }
}
```

### Transform-Level Enrich (Reusable)

Put enrichments inside named transforms to reuse them across multiple flows:

```hcl
# transforms/with_pricing.hcl
transform "with_pricing" {
  # This enrichment runs for any flow using this transform
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  # Mappings that use the enriched data
  id       = "input.id"
  name     = "input.name"
  price    = "enriched.pricing.price"
  currency = "enriched.pricing.currency"
}
```

Use it in any flow:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  transform {
    use = "transform.with_pricing"
    # Add additional fields
    fetched_at = "now()"
  }

  to {
    connector = "database"
    target    = "products"
  }
}
```

### Multiple Enrichments in Named Transforms

Combine multiple enrichments for comprehensive data:

```hcl
transform "with_full_product_data" {
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params { product_id = "input.id" }
  }

  enrich "inventory" {
    connector = "inventory_service"
    operation = "getStock"
    params { sku = "input.sku" }
  }

  enrich "reviews" {
    connector = "reviews_api"
    operation = "GET /products/reviews"
    params { product_id = "input.id" }
  }

  # Build complete product response
  id             = "input.id"
  name           = "upper(input.name)"
  price          = "enriched.pricing.price"
  currency       = "enriched.pricing.currency"
  stock          = "enriched.inventory.available"
  in_stock       = "enriched.inventory.available > 0"
  review_count   = "enriched.reviews.count"
  average_rating = "enriched.reviews.average"
}
```

### Connector Types for Enrichment

Enrichments work with any connector type:

| Connector Type | How it works |
|----------------|--------------|
| **Database** (SQLite, PostgreSQL) | Uses `Read()` to query data |
| **TCP** | Uses `Call()` for RPC-style requests |
| **HTTP** | Uses `Call()` to make HTTP requests |
| **gRPC** (coming soon) | Uses `Call()` for gRPC methods |

### Accessing Enriched Data

The `enriched` variable is a map where each key is the name you gave to the enrich block:

```cel
enriched.pricing.price           // From enrich "pricing" block
enriched.inventory.available     // From enrich "inventory" block
enriched.user.email              // From enrich "user" block
```

You can access nested data:

```cel
enriched.product.category.name   // Nested object access
enriched.pricing.tiers[0].price  // Array access
```

### Combining Input and Enriched Data

```hcl
transform {
  # Simple assignment from enriched
  price = "enriched.pricing.price"

  # Calculate using both input and enriched
  total = "double(input.quantity) * enriched.pricing.unit_price"

  # Conditional based on enriched data
  discount = "enriched.customer.is_premium ? 0.15 : 0"

  # String operations
  full_address = "enriched.address.street + ', ' + enriched.address.city"

  # Check availability
  can_fulfill = "input.quantity <= enriched.inventory.available"
}
```

## Real-World Examples

### User Registration

```hcl
transform {
  id           = "uuid()"
  email        = "lower(trim(input.email))"
  username     = "lower(replace(trim(input.username), ' ', '_'))"
  display_name = "trim(input.name)"
  password     = "hash_sha256(input.password)"
  role         = "default(input.role, 'user')"
  is_active    = "true"
  created_at   = "now()"
}
```

### Order Processing

```hcl
transform {
  order_id     = "uuid()"
  customer_id  = "input.customer_id"
  items_count  = "input.items.size()"
  subtotal     = "input.items.map(x, x.price * x.quantity).sum()"
  tax          = "output.subtotal * 0.21"
  total        = "output.subtotal + output.tax"
  status       = "input.items.size() > 10 ? 'large_order' : 'standard'"
  priority     = "input.is_premium ? 1 : input.items.size() > 5 ? 2 : 3"
  created_at   = "now()"
}
```

### Data Enrichment

```hcl
transform {
  full_name    = "input.first_name + ' ' + input.last_name"
  email_domain = "split(input.email, '@')[1]"
  age_group    = "input.age < 18 ? 'minor' : input.age < 65 ? 'adult' : 'senior'"
  initials     = "upper(substring(input.first_name, 0, 1) + substring(input.last_name, 0, 1))"
}
```

### Conditional Logic

```hcl
transform {
  discount = "input.is_member && input.total > 100 ? 0.15 : input.total > 50 ? 0.05 : 0"
  shipping = "input.country == 'US' ? (input.total > 50 ? 0 : 5.99) : 15.99"
  message  = "input.items.size() == 0 ? 'Cart is empty' : 'Ready to checkout'"
}
```

## Error Handling

CEL expressions are validated when Mycel loads the configuration:

```
$ mycel start --config ./my-service

Error: failed to compile expression for 'email': CEL compile error:
  undeclared reference to 'lowwer' (did you mean 'lower'?)
```

This means:
- **No runtime surprises**: All expressions are checked before the service starts
- **Clear error messages**: CEL provides helpful error messages with suggestions
- **Fast execution**: Compiled programs are cached and reused

## Best Practices

1. **Validate early**: Use `mycel validate` to check expressions before deployment
2. **Keep it simple**: Complex logic might be better handled in application code
3. **Use named transforms**: Reuse common patterns across flows
4. **Chain wisely**: `lower(trim(input.email))` is clearer than nested variables
5. **Test edge cases**: Consider null values and use `default()` where needed

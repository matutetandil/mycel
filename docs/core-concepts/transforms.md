# Transforms

Transforms reshape data between source and target using **CEL (Common Expression Language)** expressions. CEL is sandboxed, compiled at startup, and cached — meaning any expression error is caught before the service starts, and transforms run at near-native speed.

## Basic Transform Block

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    name       = "input.name"
    created_at = "now()"
    status     = "input.age >= 18 ? 'active' : 'pending'"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

Each line assigns a CEL expression to an output field. Expressions have access to several context variables.

## Context Variables

| Variable | Description |
|----------|-------------|
| `input` | Incoming data (request body, message payload, query result) |
| `output` | Already-computed output fields (reference previous transform results) |
| `ctx` | Request context: `ctx.user_id`, `ctx.headers`, etc. |
| `enriched` | Data fetched from external services via `enrich` blocks |
| `step` | Results of named `step` blocks |

## Named (Reusable) Transforms

Define a transform once, reference it from multiple flows:

```hcl
# transforms/user_transforms.hcl
transform "normalize_user" {
  email      = "lower(trim(input.email))"
  name       = "trim(input.name)"
  created_at = "now()"
}

transform "add_audit_fields" {
  created_by = "ctx.user_id"
  created_at = "now()"
  updated_at = "now()"
}
```

Use in a flow:

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    use = "transform.normalize_user"
    id  = "uuid()"       # Add or override fields
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

## Operators

```cel
# Arithmetic
input.price * input.quantity
input.total / 100
input.count + 1
input.balance - input.withdrawal
input.value % 2

# Comparison
input.age >= 18
input.status == "active"
input.role != "admin"

# Logical
input.active && input.verified
input.admin || input.moderator
!input.deleted

# Ternary
input.age >= 18 ? "adult" : "minor"

# String concatenation
input.first_name + " " + input.last_name
```

## Mycel Built-in Functions

### Identity and Timestamps

| Function | Returns | Description |
|----------|---------|-------------|
| `uuid()` | string | Generate a UUID v4 |
| `now()` | string | Current time in RFC3339 format |
| `now_unix()` | int | Current Unix timestamp (seconds) |

```cel
uuid()        // "550e8400-e29b-41d4-a716-446655440000"
now()         // "2025-12-29T15:04:05Z"
now_unix()    // 1735488245
```

### String Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `lower(s)` | (string) → string | Convert to lowercase |
| `upper(s)` | (string) → string | Convert to uppercase |
| `trim(s)` | (string) → string | Remove leading/trailing whitespace |
| `replace(s, old, new)` | (string, string, string) → string | Replace all occurrences |
| `substring(s, start, end)` | (string, int, int) → string | Extract substring by byte indices |
| `split(s, sep)` | (string, string) → list | Split string into list |
| `join(list, sep)` | (list, string) → string | Join list items into string |
| `len(s)` | (string) → int | String length in bytes |
| `hash_sha256(s)` | (string) → string | SHA-256 hash (hex encoded) |
| `format_date(date, fmt)` | (string, string) → string | Reformat an ISO date string |

```cel
lower(trim(input.email))
replace(lower(input.name), " ", "_")
split(input.email, "@")[1]           // Get domain part
join(input.tags, ", ")
hash_sha256(input.password)

// format_date tokens: YYYY MM DD HH mm ss
format_date(input.created_at, "YYYY-MM-DD")
format_date(now(), "DD/MM/YYYY")
```

### Default and Null Handling

| Function | Signature | Description |
|----------|-----------|-------------|
| `default(value, fallback)` | (any, any) → any | Return fallback if value is null or empty string |
| `coalesce(value, fallback)` | (any, any) → any | Alias for `default` |

```cel
default(input.nickname, input.name)
coalesce(input.phone, "N/A")
```

### Map Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `merge(m1, m2)` | (map, map) → map | Merge two (or three, or four) maps; later values win |
| `omit(m, key)` | (map, string, ...) → map | Return map without specified keys (up to 3 keys) |
| `pick(m, key)` | (map, string, ...) → map | Return map with only specified keys (up to 3 keys) |

```cel
merge(step.order, { "customer": step.customer })
omit(input, "password", "secret")
pick(input, "id", "email", "name")
```

### Array Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `first(list)` | (list) → any | First element, or null if empty |
| `last(list)` | (list) → any | Last element, or null if empty |
| `flatten(list)` | (list of lists) → list | Flatten one level of nesting |
| `unique(list)` | (list) → list | Remove duplicate values |
| `reverse(list)` | (list) → list | Reverse list order |
| `pluck(list, key)` | (list, string) → list | Extract a field from each map in a list |
| `sort_by(list, key)` | (list, string) → list | Sort list of maps by a key (ascending) |
| `sum(list)` | (list) → number | Sum numeric values in a list |
| `avg(list)` | (list) → double | Average of numeric values in a list |
| `min_val(list)` | (list) → any | Minimum value in a list |
| `max_val(list)` | (list) → any | Maximum value in a list |

```cel
first(input.items)
last(input.items)
unique(input.tags)
reverse(input.ids)
pluck(input.orders, "total")
sort_by(input.products, "price")
sum(pluck(input.items, "price"))
avg(pluck(input.scores, "value"))
```

### GraphQL Field Selection Functions

These are primarily useful when building GraphQL-aware flows to avoid over-fetching data.

| Function | Signature | Description |
|----------|-----------|-------------|
| `has_field(input, path)` | (map, string) → bool | True if a field path was requested in the GraphQL query |
| `field_requested(input, path)` | (map, string) → bool | Alias for `has_field` |
| `requested_fields(input)` | (map) → list | Get all requested field paths |
| `requested_top_fields(input)` | (map) → list | Get only top-level requested fields |

```cel
has_field(input, "orders")         // true if "orders" was queried
has_field(input, "orders.items")   // true for nested field requests
```

## CEL Standard Extensions

All CEL standard extensions are enabled.

### Strings Extension (`ext.Strings`)

```cel
"hello".charAt(1)               // "e"
"hello world".indexOf("o")      // 4
"hello world".lastIndexOf("o")  // 7
"hello".upperAscii()            // "HELLO"
"HELLO".lowerAscii()            // "hello"
"hello".replace("l", "L")       // "heLLo"
"a,b,c".split(",")              // ["a", "b", "c"]
"hello".substring(1, 4)         // "ell"
"  hello  ".trim()              // "hello"
"hello".reverse()               // "olleh"
["a", "b", "c"].join("-")       // "a-b-c"
```

### Encoders Extension (`ext.Encoders`)

```cel
base64.encode(b"hello")         // "aGVsbG8="
base64.decode("aGVsbG8=")       // b"hello"
```

### Math Extension (`ext.Math`)

```cel
math.abs(-5)           // 5
math.ceil(4.2)         // 5
math.floor(4.8)        // 4
math.round(4.5)        // 5
math.sign(-10)         // -1
math.greatest(1, 5, 3) // 5
math.least(1, 5, 3)    // 1
math.isNaN(0.0/0.0)    // true
math.isInf(1.0/0.0)    // true
```

### Lists Extension (`ext.Lists`)

```cel
[1, 2, 3, 4, 5].slice(1, 4)    // [2, 3, 4]
[[1, 2], [3, 4]].flatten()      // [1, 2, 3, 4]
lists.range(5)                  // [0, 1, 2, 3, 4]
```

### Sets Extension (`ext.Sets`)

```cel
sets.contains([1, 2, 3], [2, 3])    // true (subset check)
sets.equivalent([1, 2], [2, 1])     // true (same elements)
sets.intersects([1, 2], [2, 3])     // true (any common element)
```

## CEL Built-ins

### Type Conversions

```cel
int(input.string_number)    // "42" → 42
double(input.integer)       // 42 → 42.0
string(input.number)        // 42 → "42"
```

### String Methods

```cel
input.name.startsWith("Dr.")
input.email.endsWith("@gmail.com")
input.text.contains("keyword")
input.code.matches("[A-Z]{3}[0-9]+")  // regex match
input.name.size()                      // length
```

### List Operations

```cel
input.items.size()
input.items[0]
"admin" in input.roles        // contains check
input.items.exists(x, x > 10)
input.items.all(x, x > 0)
input.items.filter(x, x > 5)
input.items.map(x, x * 2)
```

## Enrichment in Transforms

Named transforms can include `enrich` blocks to fetch external data:

```hcl
transform "with_pricing" {
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  id       = "input.id"
  name     = "input.name"
  price    = "enriched.pricing.price"
  currency = "enriched.pricing.currency"
}
```

## Real-World Examples

### User Registration

```hcl
transform {
  id           = "uuid()"
  email        = "lower(trim(input.email))"
  username     = "lower(replace(trim(input.username), ' ', '_'))"
  password     = "hash_sha256(input.password)"
  role         = "default(input.role, 'user')"
  is_active    = "true"
  created_at   = "now()"
}
```

### Order Summary

```hcl
transform {
  order_id    = "uuid()"
  customer_id = "input.customer_id"
  items_count = "input.items.size()"
  subtotal    = "sum(pluck(input.items, 'price'))"
  tax         = "output.subtotal * 0.21"
  total       = "output.subtotal + output.tax"
  status      = "'pending'"
  created_at  = "now()"
}
```

Note that `output.subtotal` references a field computed earlier in the same transform block.

### Conditional Routing

```hcl
transform {
  tier     = "input.spend > 10000 ? 'platinum' : input.spend > 1000 ? 'gold' : 'standard'"
  discount = "input.spend > 10000 ? 0.20 : input.spend > 1000 ? 0.10 : 0"
  vip      = "input.spend > 10000"
}
```

## Error Handling

CEL expressions are compiled when Mycel loads the configuration. Errors surface before the service starts:

```
Error: failed to compile expression for 'email': CEL compile error:
  undeclared reference to 'lowwer' (did you mean 'lower'?)
```

Run `mycel validate` to catch expression errors before deployment.

# CEL Functions Reference

Complete reference for all functions available in Mycel transforms and conditions.

## Context Variables

| Variable | Description |
|----------|-------------|
| `input` | Incoming data (request body, message payload, query result) |
| `output` | Already-computed output fields in the current transform |
| `ctx` | Request context: headers, path params, user info |
| `enriched` | Data fetched from `enrich` blocks |
| `step` | Results from named `step` blocks |
| `result` | Flow result (in aspect conditions) |
| `error` | Error message string (in aspect conditions) |
| `_flow` | Flow name (in aspects) |
| `_operation` | Operation name (in aspects) |
| `_target` | Target name (in aspects) |
| `_timestamp` | Unix timestamp (in aspects) |

## Mycel Built-in Functions

### Identity and Time

| Function | Signature | Returns | Description |
|----------|-----------|---------|-------------|
| `uuid()` | `() → string` | UUID v4 string | Generate a random UUID |
| `now()` | `() → string` | RFC3339 string | Current UTC time |
| `now_unix()` | `() → int` | Unix timestamp | Current time as Unix seconds |

```cel
uuid()        // "550e8400-e29b-41d4-a716-446655440000"
now()         // "2025-12-29T15:04:05Z"
now_unix()    // 1735488245
```

### String Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `lower(s)` | `(string) → string` | Convert to lowercase |
| `upper(s)` | `(string) → string` | Convert to uppercase |
| `trim(s)` | `(string) → string` | Remove leading/trailing whitespace |
| `replace(s, old, new)` | `(string, string, string) → string` | Replace all occurrences of `old` with `new` |
| `split(s, sep)` | `(string, string) → list(string)` | Split string by separator |
| `join(list, sep)` | `(list(string), string) → string` | Join list items with separator |
| `substring(s, start, end)` | `(string, int, int) → string` | Extract substring (byte indices) |
| `len(s)` | `(string) → int` | String length in bytes |
| `hash_sha256(s)` | `(string) → string` | SHA-256 hash (hex encoded) |
| `format_date(date, fmt)` | `(string, string) → string` | Reformat ISO date string |

```cel
lower("HELLO")                          // "hello"
upper("hello")                          // "HELLO"
trim("  hello  ")                       // "hello"
replace("hello", "l", "L")             // "heLLo"
split("a,b,c", ",")                     // ["a", "b", "c"]
join(["a", "b", "c"], "-")              // "a-b-c"
substring("hello", 1, 4)               // "ell"
len("hello")                           // 5
hash_sha256("password")                // hex string

// format_date tokens: YYYY MM DD HH mm ss
format_date("2025-01-15T10:30:00Z", "YYYY-MM-DD")  // "2025-01-15"
format_date(now(), "DD/MM/YYYY HH:mm")              // "15/01/2025 10:30"
```

### Default and Null Handling

| Function | Signature | Description |
|----------|-----------|-------------|
| `default(value, fallback)` | `(any, any) → any` | Return fallback if value is null or empty string |
| `coalesce(value, fallback)` | `(any, any) → any` | Alias for `default` |

```cel
default(input.nickname, input.name)   // Use name if nickname is null/empty
coalesce(input.phone, "N/A")          // "N/A" if phone is null/empty
```

### Map Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `merge(m1, m2)` | `(map, map) → map` | Merge two maps; m2 values override m1 |
| `merge(m1, m2, m3)` | `(map, map, map) → map` | Merge three maps |
| `merge(m1, m2, m3, m4)` | `(map, map, map, map) → map` | Merge four maps |
| `omit(m, k1)` | `(map, string) → map` | Return map without key k1 |
| `omit(m, k1, k2)` | `(map, string, string) → map` | Return map without keys k1 and k2 |
| `omit(m, k1, k2, k3)` | `(map, string, string, string) → map` | Return map without up to 3 keys |
| `pick(m, k1)` | `(map, string) → map` | Return map with only key k1 |
| `pick(m, k1, k2)` | `(map, string, string) → map` | Return map with only keys k1 and k2 |
| `pick(m, k1, k2, k3)` | `(map, string, string, string) → map` | Return map with only up to 3 keys |

```cel
merge(step.order, {"customer": step.customer})
omit(input, "password")
omit(input, "password", "secret_token")
pick(input, "id", "email")
```

### Array Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `first(list)` | `(list) → any` | First element, or null if empty |
| `last(list)` | `(list) → any` | Last element, or null if empty |
| `flatten(list)` | `(list(list)) → list` | Flatten one level of nesting |
| `unique(list)` | `(list) → list` | Remove duplicate values |
| `reverse(list)` | `(list) → list` | Reverse list order |
| `pluck(list, key)` | `(list(map), string) → list` | Extract a field from each map in a list |
| `sort_by(list, key)` | `(list(map), string) → list` | Sort list of maps by field (ascending) |
| `sum(list)` | `(list(number)) → number` | Sum numeric values |
| `avg(list)` | `(list) → double` | Average of numeric values |
| `min_val(list)` | `(list) → any` | Minimum value |
| `max_val(list)` | `(list) → any` | Maximum value |

```cel
first(input.items)                         // first item or null
last(input.items)                          // last item or null
flatten([[1, 2], [3, 4]])                  // [1, 2, 3, 4]
unique([1, 2, 2, 3])                       // [1, 2, 3]
reverse([1, 2, 3])                         // [3, 2, 1]
pluck(input.orders, "total")               // [100, 200, 150]
sort_by(input.products, "price")           // sorted by price asc
sum(pluck(input.items, "price"))           // sum of prices
avg(pluck(input.scores, "value"))          // average score
min_val(pluck(input.bids, "amount"))       // minimum bid
max_val(pluck(input.bids, "amount"))       // maximum bid
```

### GraphQL Field Selection

| Function | Signature | Description |
|----------|-----------|-------------|
| `has_field(input, path)` | `(map, string) → bool` | True if field path was requested in the GraphQL query |
| `field_requested(input, path)` | `(map, string) → bool` | Alias for `has_field` |
| `requested_fields(input)` | `(map) → list(string)` | All requested field paths |
| `requested_top_fields(input)` | `(map) → list(string)` | Top-level requested fields only |

```cel
has_field(input, "orders")              // true if "orders" was queried
has_field(input, "orders.items")        // true if nested field was requested
requested_fields(input)                 // ["id", "name", "orders", "orders.total"]
requested_top_fields(input)             // ["id", "name", "orders"]
```

## CEL Standard Extensions

### ext.Strings — Extended String Operations

| Function | Signature | Description |
|----------|-----------|-------------|
| `s.charAt(i)` | `(string, int) → string` | Character at index |
| `s.indexOf(sub)` | `(string, string) → int` | First index of substring (-1 if not found) |
| `s.lastIndexOf(sub)` | `(string, string) → int` | Last index of substring |
| `s.upperAscii()` | `(string) → string` | ASCII uppercase |
| `s.lowerAscii()` | `(string) → string` | ASCII lowercase |
| `s.replace(old, new)` | `(string, string, string) → string` | Replace first occurrence |
| `s.replace(old, new, n)` | `(string, string, string, int) → string` | Replace up to n occurrences |
| `s.split(sep)` | `(string, string) → list` | Split string |
| `s.substring(start, end)` | `(string, int, int) → string` | Extract substring |
| `s.trim()` | `(string) → string` | Trim whitespace |
| `s.reverse()` | `(string) → string` | Reverse string |
| `list.join(sep)` | `(list, string) → string` | Join list with separator |

```cel
"hello".charAt(1)                    // "e"
"hello world".indexOf("o")           // 4
"hello world".lastIndexOf("o")       // 7
"hello".upperAscii()                 // "HELLO"
"hello".replace("l", "L")            // "heLLo"
"a,b,c".split(",")                   // ["a", "b", "c"]
"hello".substring(1, 4)              // "ell"
"  hello  ".trim()                   // "hello"
"hello".reverse()                    // "olleh"
["a", "b", "c"].join("-")            // "a-b-c"
```

### ext.Encoders — Base64

| Function | Signature | Description |
|----------|-----------|-------------|
| `base64.encode(bytes)` | `(bytes) → string` | Base64 encode |
| `base64.decode(s)` | `(string) → bytes` | Base64 decode |

```cel
base64.encode(b"hello")              // "aGVsbG8="
base64.decode("aGVsbG8=")            // b"hello"
```

### ext.Math — Mathematical Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `math.abs(n)` | `(number) → number` | Absolute value |
| `math.ceil(n)` | `(double) → double` | Ceiling |
| `math.floor(n)` | `(double) → double` | Floor |
| `math.round(n)` | `(double) → double` | Round to nearest |
| `math.sign(n)` | `(number) → number` | Sign (-1, 0, 1) |
| `math.greatest(...)` | `(number, ...) → number` | Maximum of arguments |
| `math.least(...)` | `(number, ...) → number` | Minimum of arguments |
| `math.isNaN(n)` | `(double) → bool` | Is NaN? |
| `math.isInf(n)` | `(double) → bool` | Is infinite? |

```cel
math.abs(-5)                         // 5
math.ceil(4.2)                       // 5.0
math.floor(4.8)                      // 4.0
math.round(4.5)                      // 5.0
math.sign(-10)                       // -1
math.greatest(1, 5, 3)               // 5
math.least(1, 5, 3)                  // 1
```

### ext.Lists — Extended List Operations

| Function | Signature | Description |
|----------|-----------|-------------|
| `list.slice(from, to)` | `(list, int, int) → list` | Slice from index to index |
| `list.flatten()` | `(list(list)) → list` | Flatten one level |
| `lists.range(n)` | `(int) → list(int)` | Generate [0, 1, ..., n-1] |

```cel
[1, 2, 3, 4, 5].slice(1, 4)          // [2, 3, 4]
[[1, 2], [3, 4]].flatten()           // [1, 2, 3, 4]
lists.range(5)                        // [0, 1, 2, 3, 4]
```

### ext.Sets — Set Operations

| Function | Signature | Description |
|----------|-----------|-------------|
| `sets.contains(a, b)` | `(list, list) → bool` | True if `a` contains all elements of `b` |
| `sets.equivalent(a, b)` | `(list, list) → bool` | True if `a` and `b` have same elements |
| `sets.intersects(a, b)` | `(list, list) → bool` | True if `a` and `b` share any element |

```cel
sets.contains([1, 2, 3], [2, 3])     // true
sets.equivalent([1, 2], [2, 1])      // true
sets.intersects([1, 2], [2, 3])      // true
```

## CEL Built-ins

### Type Conversions

```cel
int("42")                            // 42
double(42)                           // 42.0
string(42)                           // "42"
bool(1)                              // true
bytes("hello")                       // b"hello"
```

### String Methods (CEL built-ins)

```cel
"hello".startsWith("he")             // true
"hello".endsWith("lo")               // true
"hello".contains("ell")              // true
"ABC123".matches("[A-Z]{3}[0-9]+")   // true (regex)
"hello".size()                       // 5
```

### List Built-ins

```cel
[1, 2, 3].size()                     // 3
[1, 2, 3][0]                         // 1
"admin" in input.roles               // membership check

// Comprehension macros
input.items.exists(x, x.price > 100)   // any item over $100?
input.items.all(x, x.available)        // all items available?
input.items.filter(x, x.price > 50)    // items over $50
input.items.map(x, x.price * 1.1)      // apply 10% markup
```

### Operators

```cel
// Arithmetic
input.price * input.quantity
input.total + input.tax
input.balance - input.withdrawal
input.count / 2
input.value % 10

// Comparison
input.age >= 18
input.status == "active"
input.score != 0
input.amount > 100
input.priority < 5

// Logical
input.active && input.verified
input.admin || input.moderator
!input.deleted

// Conditional (ternary)
input.age >= 18 ? "adult" : "minor"

// Null coalescing
input.nickname ?? input.name
```

## Common Patterns

### Email normalization

```cel
lower(trim(input.email))
```

### Slug generation

```cel
lower(replace(trim(input.name), " ", "-"))
```

### Conditional field with fallback

```cel
default(input.display_name, input.first_name + " " + input.last_name)
```

### Extract domain from email

```cel
split(input.email, "@")[1]
```

### Compute order total

```cel
sum(pluck(input.items, "price"))
```

### Flatten and deduplicate tags

```cel
unique(flatten([input.tags, input.extra_tags]))
```

### Conditional status based on multiple fields

```cel
input.paid && input.shipped ? "completed" : input.paid ? "processing" : "pending"
```

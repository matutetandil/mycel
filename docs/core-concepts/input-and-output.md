# Input and Output

Every expression you write in a flow — a transform mapping, a `filter`, a `when`, a dedupe fingerprint — is a [CEL](../reference/cel-functions.md) expression evaluated against a set of variables Mycel provides. You never declare them. Two of them carry the data:

| Variable | What it is |
|----------|------------|
| `input` | The data that arrived. Mycel fills it from the source connector. |
| `output` | The data being built. Mycel collects it from what your expressions return. |

```hcl
transform {
  email = "lower(input.email)"
}
```

That one line reads `email` out of `input` and writes `email` into `output`. Nothing else in the config mentions either variable, which is why they are easy to miss.

## Reading: `input`

`input` is a map built by whichever connector started the flow. Its shape depends on the source, and that is the single most common source of confusion — `input.order_id` for a REST body is `input.body.order_id` for a RabbitMQ message.

```hcl
# REST source: path params, query params and body fields are all flat on
# `input`. Only headers are nested.
from {
  connector = "api"
  operation = "POST /users/:id"
}
transform {
  id     = "input.id"                  # path parameter :id
  page   = "input.page"                # query parameter ?page=
  email  = "lower(input.email)"        # body field
  origin = "input.headers['x-origin']" # request header (keys are lowercased)
}
```

```hcl
# RabbitMQ source: the message is nested under `body`
from {
  connector = "rabbit"
  operation = "orders.created"
}
transform {
  order_id = "input.body.order_id"
  route    = "input.routing_key"
}
```

Note the difference: a REST request arrives **flattened** onto `input`, while a queue message keeps its payload under `input.body` alongside the transport metadata. There is no `input.params` and no `input.query` — those were never populated by any source.

### A path or query parameter is always a string

A URL carries no types. `?include_price=true&page=2` puts the four characters `true` and the one character `2` on `input`, so the natural-looking comparison is false and the arithmetic fails:

```hcl
# ?include_price=true
when = "input.include_price == true"     # false — a string is not a boolean
when = "input.include_price == \"true\""  # this is the one

# ?page=2
page = "input.page + 1"                  # fails — no such overload
page = "int(input.page) + 1"             # convert it first
```

The same flag sent in a **JSON body** is a real boolean and does compare to `true`, because JSON has types and a query string does not. So the right expression depends on where the value came in, not on what it looks like.

**[Source Properties by Connector](../reference/source-properties.md) lists exactly what `input` holds for each of the 15 source types** — including the metadata fields (`input._topic`, `input._path`, `input.partition`, `input.new` / `input.old` for CDC) that only some sources set. Check it before guessing.

## Writing: the left-hand side is `output`

Each line in a `transform` block names one output field:

```hcl
transform {
  id         = "uuid()"
  email      = "lower(input.email)"
  created_at = "now()"
}
# → {"id": "...", "email": "...", "created_at": "..."}
```

The field name goes on the left, bare. **You never write `output.` on the left-hand side.** HCL attribute names are single identifiers, so a dotted name is not a Mycel-level error — the file does not parse at all:

```hcl
transform {
  output.id = "uuid()"   # ✗
}
```

```
Error: HCL parse error: flows/users.mycel:7,5-11: Argument or block
definition required; An argument or block definition is required here.
```

Attribute names cannot be quoted either (`"customer.name" = ...` is also a parse error). To produce a nested object, return one from a single expression:

```hcl
transform {
  id       = "uuid()"
  customer = "{'name': input.name, 'email': lower(input.email)}"
}
# → {"id": "...", "customer": {"name": "...", "email": "..."}}
```

## Referring to `output`

On the **right-hand side**, `output` is readable — but what it holds depends on which block you are in, because each block runs at a different point in the pipeline.

| Where | `output` holds |
|-------|----------------|
| `transform { }` | The fields computed **above this line** in the same block. Starts empty. |
| `to { when = ... }` | The finished result of the flow's `transform`. |
| `to { transform { } }` | The finished result of the flow's `transform` (this block reshapes it per destination). |
| `dedupe { fingerprint { } }` | The finished result of the flow's `transform`. |
| `to { transaction { } }` | The finished result of the flow's `transform`. |
| `response { }` | The result the **destination returned** — rows from a query, the API's reply. |

So the same word means "what I have built so far" inside a transform and "what came back" inside a response:

```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  transform {
    customer_id = "input.customer_id"
    subtotal    = "sum(pluck(input.items, 'price'))"
    tax         = "output.subtotal * 0.21"   # output = fields above
  }

  to {
    connector = "db"
    target    = "orders"
    when      = "output.subtotal > 0"        # output = the transform result
  }

  response {
    order_id = "output.id"                   # output = what the database returned
    status   = "'created'"
  }
}
```

### Order of evaluation

Within a `transform` (and any other block whose fields are CEL expressions), fields are evaluated **top to bottom in the order you wrote them**. Each result is added to `output` before the next line runs, so a line may reference any field above it and none below it:

```hcl
transform {
  subtotal = "sum(pluck(input.items, 'price'))"
  tax      = "output.subtotal * 0.21"        # ✓ subtotal is above
  total    = "output.subtotal + output.tax"  # ✓ both are above
}
```

```hcl
transform {
  total    = "output.subtotal + output.tax"  # ✗ neither exists yet
  subtotal = "sum(pluck(input.items, 'price'))"
  tax      = "output.subtotal * 0.21"
}
```

A forward reference is not an error — the field is simply absent, and the expression sees a missing key. Reorder the block, or guard with `has(output.subtotal)`.

!!! note "Fixed in 2.17.0"
    Before 2.17.0, field order was not preserved and a backward reference
    resolved non-deterministically: the same config and the same payload
    could compute `tax` before `subtotal` on one message and after it on the
    next. If you worked around this by flattening chained fields into one
    long expression, you no longer need to.

## Quote your expressions

An expression is written as a **quoted string**. Mycel evaluates the string contents as CEL at runtime; HCL never sees the expression itself.

```hcl
transform {
  id    = "uuid()"                # ✓
  email = "lower(input.email)"    # ✓
}
```

Leave the quotes off and HCL parses the expression itself first, which rejects most real CEL:

```hcl
transform {
  status = 'pending'                            # ✗ Single quotes are not valid
  big    = input.items.filter(i, i.price > 10)  # ✗ Missing newline after argument
}
```

CEL string literals are single-quoted and CEL macros are not HCL syntax, so both fail at parse time. Quote every expression and neither can happen.

## The other context variables

`input` and `output` are the two you use constantly. The rest are filled in by specific blocks and are empty outside them:

| Variable | Filled by | Available in |
|----------|-----------|--------------|
| `constants` | [`constants` blocks](constants.md) — `constants.<name>`, the same in an HCL attribute | every expression, and every attribute |
| `enriched` | [`enrich` blocks](transforms.md#enrichment-in-transforms) — `enriched.<name>` per block | `transform`, and anything after it |
| `step` | [`step` blocks](../guides/multi-step-flows.md) — `step.<name>` per step | `transform`, later `step` blocks, `to` |
| `error` | A failure, as `error.message` / `error.code` / `error.type` | `error_handling`, `on_error` aspects |
| `drop` | A deflected message, as `drop.reason` / `drop.policy` / `drop.message_id` | `on_drop` aspects |
| `result` | The flow's result | `after` aspects |
| `_flow`, `_operation`, `_target`, `_timestamp` | Flow metadata | [Aspects](aspects.md) |

!!! warning "`ctx` is reserved but not populated"
    `ctx` is declared in the expression environment and older examples show
    `ctx.user_id` and `ctx.headers`, but nothing ever fills it — it always
    evaluates as an empty map. Read request headers from `input.headers`
    instead, and authenticated user data from wherever your
    [auth configuration](../guides/auth.md) puts it.

## See also

- [Source Properties by Connector](../reference/source-properties.md) — what `input` holds, per source
- [Transforms](transforms.md) — the full `transform` block, enrichment, named transforms
- [CEL Functions Reference](../reference/cel-functions.md) — every function available inside an expression
- [Flows](flows.md) — where each block sits in the pipeline

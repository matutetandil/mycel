# Constants

A `constants` block declares a value once and lets the rest of the
configuration refer to it by name.

```hcl
constants {
  skus_to_skip = ["SKU-1", "SKU-2", "SKU-3"]
  page_size    = 500
  region       = env("REGION", "us")
}
```

Every part of a configuration reads them the same way — `constants.<name>`:

```hcl
flow "list_items" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    query     = "SELECT * FROM items LIMIT ${constants.page_size}"
  }
}

flow "process_item" {
  from {
    connector = "rabbit"
    operation = "items.updated"
  }

  accept {
    when      = "!(input.body.sku in constants.skus_to_skip)"
    on_reject = "ack"
  }

  transform {
    sku    = "input.body.sku"
    region = "constants.region"
  }

  to {
    connector = "db"
    target    = "items"
  }
}
```

## One name, two machines

The two references above are evaluated by different things. `${...}` is HCL's,
folded in when the configuration is read; `constants.region` inside a transform
is CEL's, evaluated for every message. A constant is put in front of both, so
which one you are writing for is not something you have to know.

That is the whole point of the block. A constant that resolved in a `query` and
not in a `filter` would be worse than having none, because the failure is
per-expression and nothing announces it.

## What a constant holds

A literal: a string, a number, a boolean, a list, a map, or anything built out
of those — including an `env()` call, which is a literal by the time anything
else runs.

```hcl
constants {
  retries      = 3
  debug        = false
  regions      = ["us", "uk", "fr"]
  thresholds   = { warn = 100, alert = 500 }
  database_url = env("DATABASE_URL")
}
```

What a constant is **not** is a value worked out from a message. There is no
`constants.total = "input.price * input.quantity"`: that is what a
[transform](transforms.md) is for. Constants are read once, when the
configuration is, and do not change while the service runs.

## Where they can be declared

In any `.mycel` file, as many blocks as you like. They are collected before
anything else is read, so a flow may use a constant a file later in the
directory declares — nothing about the order files are walked in matters.

```hcl
# constants.mycel
constants {
  page_size = 500
}
```

Declaring the same name twice is refused, naming both files:

```
constant "page_size" is declared twice: in constants.mycel and in limits.mycel
— a constant holds one value, so which file wins cannot be left to the order
they are read in
```

## Writing one

```bash
mycel add constants --value page_size=500 --value region=us
```

## Checking them

`mycel validate` lists what it found:

```
  Constants: 3
    - page_size = 500
    - region = us
    - skus_to_skip = [SKU-1 SKU-2 SKU-3]
```

## See also

- [Input and Output](input-and-output.md) — the other names an expression can use
- [Transforms](transforms.md) — for values computed per message
- [Reusable Blocks](reusable-blocks.md) — for reusing a *block* rather than a value

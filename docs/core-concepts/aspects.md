# Aspects

Aspects are Mycel's mechanism for **cross-cutting concerns** — behavior that applies across many flows rather than living inside any single one. Audit logging, metrics, error alerting, response enrichment, deprecation notices: instead of repeating that logic in every flow, you declare it once as an aspect and bind it to flows by name pattern.

Aspects are a core, fully declarative part of the model — no plugins or external code involved. An `aspect` is a top-level block, like `flow`, `connector`, or `transform`, and it operates on the flow pipeline: Mycel matches an aspect's pattern against your flow names and weaves its behavior in at the requested point.

```hcl
aspect "audit_log" {
  when = "after"
  on   = ["create_*", "update_*"]

  action {
    connector = "audit_db"
    operation = "INSERT audit_logs"
    transform {
      flow      = "_flow"
      user_id   = "ctx.user_id"
      action    = "_operation"
      timestamp = "_timestamp"
    }
  }
}
```

That one block adds an audit-log write after every `create_*` and `update_*` flow — without touching a single flow definition.

## Aspect Timing

| `when` | Description |
|--------|-------------|
| `before` | Run before the flow executes |
| `after` | Run after the flow succeeds |
| `around` | Wrap the entire flow execution |
| `on_error` | Run when the flow fails |

## Aspect Variables

In aspect action transforms:

| Variable | Description |
|----------|-------------|
| `_flow` | Flow name |
| `_operation` | HTTP method or operation name |
| `_target` | Target connector/resource |
| `_timestamp` | Unix timestamp |
| `result` | Flow result (after/on_error) |
| `error.message` | Error message string (on_error) |
| `error.code` | HTTP status code, e.g. 404, 500 (on_error) |
| `error.type` | Error category: `http`, `timeout`, `connection`, `validation`, `not_found`, `auth`, `flow`, `unknown` (on_error) |

## Flow Invocation

Aspect actions can invoke flows directly instead of writing to connectors. Use `flow` instead of `connector` in the action block:

```hcl
aspect "notify_on_create" {
  when = "after"
  on   = ["create_*"]

  action {
    flow = "send_notification"
    transform {
      message = "'Created: ' + _flow"
      user_id = "input.user_id"
    }
  }
}
```

The invoked flow receives the transform output as its input. This is useful for:
- **Flow orchestration** — chain flows through aspects without coupling them directly
- **Internal flows** — flows without a `from` block that are only invocable from aspects
- **Error handling** — invoke recovery flows on failure

`connector` and `flow` are mutually exclusive in an action block.

## Response Enrichment

After aspects can include a `response` block to inject fields into every row of the flow result. This is useful for API versioning, deprecation notices, or adding metadata without modifying individual flows:

```hcl
aspect "v1_deprecation" {
  when = "after"
  on   = ["*_v1"]

  response {
    headers = {
      Deprecation = "true"
      Sunset      = "Thu, 01 Jun 2026 00:00:00 GMT"
    }

    _warning = "'This API version is deprecated. Migrate to v2.'"
  }
}
```

The `response` block supports two types of enrichment:
- **Body fields** — CEL expressions merged into every row of the response. Have access to `result.data`, `result.affected`, `input`, `_flow`, and `_operation`
- **Headers** — key-value pairs set as HTTP headers (or protocol equivalent for gRPC metadata, etc.). Values are literal strings

The `response` block is only valid for `after` aspects. An aspect can have both an `action` and a `response` block — the action runs as a side-effect and the response enriches the output.

## Pattern Matching

The `on` attribute accepts glob patterns matching flow names:

```hcl
on = ["*"]                          # All flows
on = ["create_*", "update_*"]       # All create and update flows
on = ["get_product*"]               # Flows starting with "get_product"
```

## See Also

- [Flows](flows.md) — the unit of work aspects bind to
- [Transforms](transforms.md) — the CEL expressions used inside aspect actions
- [Error Handling](../guides/error-handling.md) — retries, DLQ, and `on_error` dispositions that complement `on_error` aspects
- [Aspects example](../../examples/aspects)

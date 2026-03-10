# Plugins

Plugins add new connector types, validators, and sanitizers to Mycel via WASM modules. They let you integrate systems not natively supported — Salesforce, SAP, proprietary protocols, or custom security rules.

## Declaring a Plugin

```hcl
plugin "salesforce" {
  source  = "github.com/acme/mycel-salesforce"
  version = "^1.0"
}
```

After declaration, plugin-provided connectors, validators, and sanitizers work exactly like built-in ones.

## Using a Plugin Connector

```hcl
connector "sf" {
  type         = "salesforce"     # Provided by the plugin
  instance_url = env("SF_URL")
  token        = env("SF_TOKEN")
}

flow "sync_contacts" {
  from {
    connector = "api"
    operation = "POST /sync"
  }
  to {
    connector = "sf"
    operation = "upsert_contact"
  }
}
```

## Plugin Sources

| Format | Example |
|--------|---------|
| GitHub | `github.com/org/repo` |
| GitLab | `gitlab.com/org/repo` |
| Bitbucket | `bitbucket.org/org/repo` |
| Local path | `./plugins/my-plugin` |
| Any git URL | `https://git.internal.com/repo` |

## Version Constraints

| Constraint | Meaning |
|------------|---------|
| `"^1.0"` | Compatible with 1.x (`>= 1.0, < 2.0`) |
| `"~2.0"` | Patch-level updates (`>= 2.0, < 2.1`) |
| `">= 1.0, < 3.0"` | Explicit range |
| `"1.2.3"` | Exact version |
| `"latest"` | Latest release |

## Plugin Management

```bash
mycel plugin install             # Install all declared plugins
mycel plugin list                # Show installed plugins and versions
mycel plugin remove salesforce   # Remove a specific plugin
mycel plugin update              # Update all plugins to latest compatible versions
```

Plugins are cached in `mycel_plugins/` (add to `.gitignore`). Reproducible builds use `plugins.lock`.

## Auto-Install

Plugins are resolved and installed automatically when you run `mycel start`. If the plugin is already cached and the version constraint is satisfied, it uses the cache.

## Writing a Plugin

Plugin authors create a directory with a `plugin.hcl` manifest and WASM binaries.

### plugin.hcl Manifest

```hcl
plugin {
  name    = "salesforce"
  version = "1.0.0"
}

provides {
  # New connector type
  connector "salesforce" {
    wasm = "connector.wasm"
  }

  # Custom validator
  validator "sf_id" {
    wasm       = "validators.wasm"
    entrypoint = "validate_sf_id"
    message    = "Invalid Salesforce ID format"
  }

  # Custom sanitizer
  sanitizer "pii_filter" {
    wasm       = "sanitizers.wasm"
    entrypoint = "filter_pii"
    apply_to   = ["flows/api/*"]
    fields     = ["email", "phone", "ssn"]
  }
}
```

### WASM Interface

Plugin WASM modules must implement the standard Mycel interface:

```
# Memory management
alloc(size: i32) -> i32
free(ptr: i32, size: i32)

# Connector operations (connector.wasm)
read(ptr: i32, len: i32) -> i32
write(ptr: i32, len: i32) -> i32
call(ptr: i32, len: i32) -> i32

# Validator (validators.wasm)
entrypoint_name(ptr: i32, len: i32) -> i32   # returns 1 (valid) or 0 (invalid)

# Sanitizer (sanitizers.wasm)
entrypoint_name(ptr: i32, len: i32) -> i32   # returns sanitized JSON
```

Input and output are JSON-encoded. The host allocates memory using `alloc`, passes a pointer and length, and reads back the result at the returned pointer.

See [WASM Documentation](wasm.md) for complete interface details and language-specific examples.

## See Also

- [WASM Documentation](wasm.md) — WASM interface spec, 6 language examples
- [Extending Mycel](../guides/extending.md) — validators, functions, mocks
- [Plugin example](../../examples/plugin)

# Plugin Example

A connector type that is not in the runtime, loaded from a plugin and used by
flows like any other.

## Run it

```bash
mycel start --config examples/plugin
```

```bash
# What the plugin holds
curl localhost:3000/stock
# [{"on_hand":10,"sku":"WIDGET-1","warehouse":"auckland"}, ...]

# Reserve some — an operation that is neither a read nor a write
curl -X POST localhost:3000/stock/reserve \
  -H 'Content-Type: application/json' \
  -d '{"sku":"WIDGET-1","quantity":4}'
# {"remaining":6,"reserved":4,"sku":"WIDGET-1"}

# The plugin's own rule, enforced inside the module
curl -X POST localhost:3000/stock/reserve \
  -H 'Content-Type: application/json' \
  -d '{"sku":"WIDGET-2","quantity":99}'
# {"error":"... only 3 of WIDGET-2 on hand, 99 asked for"}

# Adjust a level
curl -X POST localhost:3000/stock \
  -H 'Content-Type: application/json' \
  -d '{"sku":"WIDGET-2","delta":25}'
```

## What is where

| File | What it is |
|---|---|
| `plugins.mycel` | Which plugins to load, and where they live |
| `plugins/inventory-store/plugin.mycel` | What that plugin provides — its manifest |
| `plugins/inventory-store/src/lib.rs` | The connector itself, in Rust |
| `connectors/inventory.mycel` | A connector of the type the plugin brought |
| `flows/stock.mycel` | Flows naming it: read, write, and a `step` calling `reserve` |

`plugins/example-plugin/` holds a manifest with no module beside it, kept as an
illustration of the shape.

See [plugins/inventory-store/README.md](plugins/inventory-store/README.md) for
the interface a plugin implements and how to build one.

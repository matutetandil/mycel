# Project Structure

## The only rule

Mycel reads **every file ending in `.mycel`**, recursively, from one
configuration directory, and merges them into a single service definition.

That is the whole loading model. There is no manifest, no include list, no entry
point file. If a `.mycel` file is under the config directory, it is part of your
service; if it is not, it is invisible to Mycel no matter what it contains.

```bash
mycel start --config ./my-service     # explicit
mycel start                           # defaults to the current directory
docker run -v $(pwd):/etc/mycel mdenda/mycel   # the image reads /etc/mycel
```

Two consequences worth internalising:

- **Files are merged, not scoped.** A connector declared in `connectors/db.mycel`
  is visible to a flow in `flows/orders/create.mycel`. Splitting files is purely
  for your benefit as a reader.
- **Directory and file names mean nothing to the runtime.** `connectors/`,
  `flows/`, `aspects/` are conventions this documentation recommends because
  they help humans, not because Mycel looks for them. Putting everything in one
  `service.mycel` produces an identical service.

## Recommended layout

Start with one file and split when a file stops fitting on a screen. These are
the three shapes that show up in practice.

=== "Single file"

    Fine for a demo, a proof of concept, or a service with one or two flows.

    ```
    my-service/
      service.mycel      # service, connectors, flows — everything
    ```

=== "Small service"

    The split most services stay at. One file per kind.

    ```
    my-service/
      config.mycel       # service {} block: name, version, ports
      connectors.mycel   # every connector
      flows.mycel        # every flow
      types.mycel        # type {} definitions, if any
    ```

=== "Real project"

    One file per *thing*, grouped by kind. This is the layout used by the
    production consumers Mycel was built against.

    ```
    my-service/
      config.mycel
      connectors/
        rabbit.mycel
        magento_db.mycel
        slack.mycel
      flows/
        style_create.mycel
        style_update.mycel
      aspects/
        slack_notifier.mycel
        slack_error_notifier.mycel
      shared/
        order_lock.mycel      # named reusable blocks
    ```

    Past a dozen flows, group them by domain rather than by kind —
    `flows/orders/`, `flows/inventory/`. Nesting depth is unlimited and costs
    nothing.

**The guideline that matters:** name the file after what it declares. When a
flow misbehaves, you want `flows/style_update.mycel` to be an obvious place to
look. Mycel will not help you here — it reports errors by file path, and that
path is only useful if you chose it well.

## Names are global

Because every file is merged, **names must be unique across the whole
directory**, not per file. Two connectors called `db` in two different files is
an error, caught at parse time:

```
duplicate connector name "db"
```

This applies to connectors, flows, types, transforms, aspects and every kind of
named reusable block. It is the one way file organisation can bite you, and it
fails loudly rather than silently picking one.

## Files Mycel does *not* read

| Path | What happens |
|---|---|
| Anything not ending in `.mycel` | Ignored, with the exceptions below |
| `mycel_plugins/` | Skipped entirely — this is Mycel's own plugin cache, not your config |
| A `.mycel` plugin *manifest* | Skipped by the config parser and read by the plugin loader instead. Identified by content, not name: a top-level `plugin { }` block with no label plus a `provides { }` block. A plugin *declaration* in your config — `plugin "name" { source = "..." }`, with a label — is ordinary config and is read normally |

So a `README.md`, a `.sql` file or a shell script can live beside your config
without any effect. Two non-`.mycel` file types *are* meaningful, but only
because other subsystems look for them at fixed paths:

- **`.env`** — read from `<config-dir>/.env`, falling back to `./.env`. Loaded
  before parsing so `env()` resolves. Never overrides variables already set in
  the environment.
- **Mock fixtures** — JSON, at paths that *are* structural, since the mock
  loader resolves them by name rather than scanning:

    | Path | Serves |
    |---|---|
    | `mocks/connectors/{connector}/{target}.json` | A read against that target |
    | `mocks/connectors/{connector}/{METHOD}_{path}.json` | An HTTP call, e.g. `GET_users.json` |
    | `mocks/flows/{flow_name}.json` | A whole flow's output |

    These are the only paths where a directory name is load-bearing.

## Per-environment configuration

There is **no per-environment directory**. Running the same config against
development and production is done in two ways, both inside your `.mycel` files:

1. **`env()`** for anything that differs by value — hosts, credentials, ports.
   Give it a default where one makes sense: `env("DB_HOST", "localhost")`.
2. **[Connector profiles](../connectors/profile.md)** for anything that differs
   by *shape* — a connector that is a local database in development and a
   remote API in production.

`MYCEL_ENV` selects [environment-aware defaults](../core-concepts/environments.md)
(log level, log format, and similar) and can drive profile selection. It does
not load a different set of files.

## Checking your structure

`mycel validate` parses the whole directory and reports what it found. Run it
before anything else — it needs no connections and no deployment environment:

```bash
mycel validate --config ./my-service
```

```
✓ Configuration is valid!

  Connectors: 3
    - rabbit (mq)
    - magento_db (database)
    - slack (notification)
  Flows: 2
    - style_create: * → styles
    - style_update: * → styles
  Types: 0
```

If a connector or flow you expected is missing from that list, the file
declaring it is not being read — check the extension first, then that it is
actually under `--config`.

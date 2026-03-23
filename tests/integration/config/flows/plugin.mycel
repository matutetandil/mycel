# Plugin test flows

# Initialize plugin_results table
flow "plugin_init" {
  from {
    connector = "api"
    operation = "POST /test/plugin-init"
  }
  step "create_table" {
    connector = "sqlite"
    query     = "CREATE TABLE IF NOT EXISTS plugin_results (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, code TEXT NOT NULL)"
  }
  transform {
    name = "'__init__'"
    code = "'init'"
  }
  to {
    connector = "sqlite"
    target    = "plugin_results"
  }
}

# Validate input using plugin's WASM validator (always_valid on `code` field)
flow "plugin_validate" {
  from {
    connector = "api"
    operation = "POST /test/plugin-validate"
  }

  validate {
    input = "type.plugin_validated"
  }

  transform {
    name = "input.name"
    code = "input.code"
  }

  to {
    connector = "sqlite"
    target    = "plugin_results"
  }
}

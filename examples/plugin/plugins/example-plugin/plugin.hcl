// Example Plugin Manifest
// This file describes the plugin and what it provides to Mycel.

plugin {
  name        = "example"
  version     = "1.0.0"
  description = "Example plugin demonstrating the Mycel plugin system"
  author      = "Mycel Team"
  license     = "MIT"
}

provides {
  // Connector definition
  // When this plugin is loaded, the "example" connector type becomes available.
  connector "example" {
    // Path to the WASM module (relative to this plugin directory)
    wasm = "connector.wasm"

    // Configuration schema for this connector
    // These fields will be available in the connector {} block
    config {
      // Simple string field
      api_key = "string"

      // String with description
      endpoint = {
        type        = "string"
        description = "The API endpoint URL"
      }

      // Required field with sensitivity marker
      secret = {
        type      = "string"
        required  = true
        sensitive = true
      }

      // Field with default value
      timeout = {
        type    = "number"
        default = 30
      }

      // Boolean field
      debug = {
        type    = "bool"
        default = false
      }
    }
  }

  // Optional: Custom functions for CEL expressions
  // Uncomment and provide functions.wasm to enable
  // functions {
  //   wasm    = "functions.wasm"
  //   exports = ["example_format", "example_parse"]
  // }
}

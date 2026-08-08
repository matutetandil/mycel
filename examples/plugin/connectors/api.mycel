// Example Plugin Connector
// This demonstrates how to use a connector provided by a plugin.
// Note: This requires the plugin's connector.wasm to be present.

// REST API for exposing endpoints
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
  }
}

// Plugin connector (uses the "example" type from the plugin)
// This won't work without the actual connector.wasm file,
// but shows the configuration pattern.
//
// connector "example_api" {
//   type = "example"  // Type comes from the plugin
//
//   // These config fields are defined in the plugin manifest
//   api_key  = env("EXAMPLE_API_KEY")
//   endpoint = "https://api.example.com"
//   secret   = env("EXAMPLE_SECRET")
//   timeout  = 60
//   debug    = true
// }

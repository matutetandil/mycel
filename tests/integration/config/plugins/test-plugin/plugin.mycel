plugin {
  name        = "test-plugin"
  version     = "1.0.0"
  description = "Integration test plugin"
}

provides {
  validator "always_valid" {
    wasm       = "validators.wasm"
    entrypoint = "validate_always_valid"
    message    = "Always valid test validator"
  }
}

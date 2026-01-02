// WASM Validators
// These validators use WebAssembly modules for complex validation logic.

// Note: To use this validator, you need to build the WASM module first.
// See README.md for instructions.

validator "argentina_cuit" {
  type       = "wasm"
  wasm       = "./validators/cuit_validator.wasm"
  entrypoint = "validate"
  message    = "Invalid Argentine CUIT (must be 11 digits with valid check digit)"
}

// You can also combine WASM validators with regex/CEL validators
validator "email" {
  type    = "regex"
  pattern = "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
  message = "Invalid email format"
}

validator "adult_age" {
  type    = "cel"
  expr    = "value >= 18 && value <= 120"
  message = "Age must be between 18 and 120"
}

// Mycel WASM Validator Example
// Demonstrates custom validators using WebAssembly.

service {
  name    = "wasm-validator-demo"
  version = "1.0.0"
}

connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/demo.db"
}

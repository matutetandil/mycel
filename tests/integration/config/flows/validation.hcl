# Validation test flows

flow "validated_create" {
  from {
    connector = "api"
    operation = "POST /test/validate"
  }

  validate {
    input = "type.user"
  }

  transform {
    name  = "input.name"
    email = "input.email"
  }

  to {
    connector = "postgres"
    target    = "validate_results"
  }
}

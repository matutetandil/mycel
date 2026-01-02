// Type Definitions with Custom Validators
// Shows how to reference validators in type definitions.

type "user" {
  id       = number
  username = string
  email    = string
  password = string
  age      = number
  phone    = string
  status   = string
}

type "product" {
  id    = number
  name  = string
  slug  = string
  price = number
}

// Note: The validate attribute will be supported in the type parser.
// For now, validators are defined and can be used programmatically.
// Future syntax:
//
// type "user" {
//   email    = string { validate = "validator.email" }
//   password = string { validate = "validator.strong_password" }
//   age      = number { validate = "validator.adult_age" }
//   phone    = string { validate = "validator.phone_ar" }
//   status   = string { validate = "validator.valid_status" }
// }

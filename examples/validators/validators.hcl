// Custom Validators
// Defines reusable validation logic using regex and CEL expressions.

// Regex Validators - Simple pattern matching

validator "email" {
  type    = "regex"
  pattern = "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
  message = "Invalid email format"
}

validator "phone_ar" {
  type    = "regex"
  pattern = "^\\+54[0-9]{10,11}$"
  message = "Invalid Argentine phone number (expected +54 followed by 10-11 digits)"
}

validator "uuid" {
  type    = "regex"
  pattern = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
  message = "Invalid UUID format"
}

validator "slug" {
  type    = "regex"
  pattern = "^[a-z0-9]+(?:-[a-z0-9]+)*$"
  message = "Invalid slug format (lowercase letters, numbers, and hyphens only)"
}

validator "username" {
  type    = "regex"
  pattern = "^[a-zA-Z][a-zA-Z0-9_]{2,19}$"
  message = "Username must start with a letter, 3-20 chars, letters/numbers/underscore only"
}

// CEL Validators - Expression-based validation

validator "adult_age" {
  type    = "cel"
  expr    = "value >= 18 && value <= 120"
  message = "Age must be between 18 and 120"
}

validator "positive_number" {
  type    = "cel"
  expr    = "value > 0"
  message = "Value must be positive"
}

validator "valid_status" {
  type    = "cel"
  expr    = "value in ['pending', 'active', 'suspended', 'cancelled']"
  message = "Invalid status (expected: pending, active, suspended, cancelled)"
}

validator "strong_password" {
  type = "cel"
  expr = <<-CEL
    size(value) >= 8 &&
    value.matches("[A-Z]") &&
    value.matches("[a-z]") &&
    value.matches("[0-9]") &&
    value.matches("[!@#$%^&*]")
  CEL
  message = "Password must have 8+ chars with uppercase, lowercase, number, and special char"
}

validator "future_date" {
  type    = "cel"
  expr    = "timestamp(value) > now()"
  message = "Date must be in the future"
}

validator "reasonable_price" {
  type    = "cel"
  expr    = "value >= 0.01 && value <= 999999.99"
  message = "Price must be between 0.01 and 999999.99"
}

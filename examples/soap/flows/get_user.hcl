# Get user by email via SOAP
#
# SOAP operation: GetUser
# Expects: <Email>john@example.com</Email>
# Returns: user row from database

flow "get_user" {
  from {
    connector = "soap_server"
    operation = "GetUser"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

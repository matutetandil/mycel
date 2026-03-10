# Create user via SOAP
#
# SOAP operation: CreateUser
# Expects: <Email>...</Email> <Name>...</Name>
# Returns: created user row

flow "create_user" {
  from {
    connector = "soap_server"
    operation = "CreateUser"
  }

  validate {
    input = "type.user"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

# SOAP flows

flow "soap_create_item" {
  from {
    connector = "soap_server"
    operation = "CreateItem"
  }
  to {
    connector = "postgres"
    target    = "items"
  }
}

flow "soap_get_item" {
  from {
    connector = "soap_server"
    operation = "GetItem"
  }
  to {
    connector = "postgres"
    target    = "items"
  }
}

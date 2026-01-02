// Flow definitions

flow "create_company" {
  from {
    connector = "api"
    operation = "POST /companies"
  }

  validate {
    input = "type.company"
  }

  to {
    connector = "db"
    target    = "companies"
  }
}

flow "get_companies" {
  from {
    connector = "api"
    operation = "GET /companies"
  }
  to {
    connector = "db"
    target    = "companies"
  }
}

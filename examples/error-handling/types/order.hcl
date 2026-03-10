# Order input type for validation

type "order" {
  product_id = string { required = true }
  quantity   = number { min = 1, max = 100 }
  email      = string { format = "email" }
}

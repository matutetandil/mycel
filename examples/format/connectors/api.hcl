# REST API connector (default format: JSON)
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}

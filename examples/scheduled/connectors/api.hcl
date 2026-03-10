# REST API connector configuration
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET"]
  }
}

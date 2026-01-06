# Configuration for File -> RabbitMQ integration example

service {
  name        = "file-to-rabbit-example"
  version     = "1.0.0"
  description = "Integration pattern: Read files periodically and publish to RabbitMQ"
}

environment "development" {
  variables {
    FILES_BASE_PATH = "./data"
    RABBIT_HOST     = "localhost"
    RABBIT_PORT     = "5672"
    RABBIT_USER     = "guest"
    RABBIT_PASS     = "guest"
    AWS_REGION      = "us-east-1"
    S3_BUCKET       = "local-test"
    S3_ENDPOINT     = "http://localhost:9000"
    S3_FORCE_PATH_STYLE = "true"
    AWS_ACCESS_KEY  = "minioadmin"
    AWS_SECRET_KEY  = "minioadmin"
  }
}

environment "production" {
  variables {
    FILES_BASE_PATH = "/data"
    RABBIT_HOST     = "${RABBIT_HOST}"
    RABBIT_PORT     = "${RABBIT_PORT}"
    RABBIT_USER     = "${RABBIT_USER}"
    RABBIT_PASS     = "${RABBIT_PASS}"
    AWS_REGION      = "${AWS_REGION}"
    S3_BUCKET       = "${S3_BUCKET}"
    AWS_ACCESS_KEY  = "${AWS_ACCESS_KEY}"
    AWS_SECRET_KEY  = "${AWS_SECRET_KEY}"
  }
}

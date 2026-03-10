# RabbitMQ connector for dead letter queue (DLQ)

connector "rabbit" {
  type     = "queue"
  driver   = "rabbitmq"
  url      = env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
  exchange = "error_handling_example"
}

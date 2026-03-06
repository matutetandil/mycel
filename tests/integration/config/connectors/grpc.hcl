# gRPC server
connector "grpc_api" {
  type   = "grpc"
  driver = "server"

  host        = "0.0.0.0"
  port        = 50051
  proto_path  = "/etc/mycel/protos"
  proto_files = ["test.proto"]
  reflection  = true
}

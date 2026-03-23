# SFTP connector for integration tests
connector "sftp_server" {
  type      = "ftp"
  protocol  = "sftp"
  host      = env("SFTP_HOST", "localhost")
  port      = env("SFTP_PORT", 22)
  username  = env("SFTP_USER", "testuser")
  password  = env("SFTP_PASS", "testpass")
  base_path = "/upload"
}

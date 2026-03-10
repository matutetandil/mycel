# SOAP server connector
#
# Exposes SOAP operations on port 8080 at /ws.
# Auto-generates WSDL at GET /wsdl.

connector "soap_server" {
  type         = "soap"
  port         = 8080
  soap_version = "1.1"
  namespace    = "http://example.com/users"
}

# Circuit breaker aspect
#
# Protects database flows from cascading failures.
# After 5 consecutive failures, the circuit opens for 30s.
# During that time, requests fail fast without hitting the database.

aspect "db_circuit_breaker" {
  on   = ["*"]
  when = "around"

  circuit_breaker {
    name              = "postgres_cb"
    failure_threshold = 5
    success_threshold = 2
    timeout           = "30s"
  }
}

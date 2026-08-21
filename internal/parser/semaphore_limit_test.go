package parser

import "testing"

// The reference page's semaphore example, parsed.
//
// It writes `limit = 10` and says in the next sentence that `limit` and
// `max_permits` are the same setting under two names. They were not: only
// max_permits was accepted, so a semaphore written the documented way was
// refused as an unsupported argument. The claim is true now.
func TestASemaphoreAcceptsLimitAsWellAsMaxPermits(t *testing.T) {
	for _, written := range []struct {
		name string
		body string
	}{
		{"limit", `limit = 10`},
		{"max_permits", `max_permits = 10`},
	} {
		t.Run(written.name, func(t *testing.T) {
			config, err := parseString(t, `
connector "api" {
  type = "rest"
  port = 8080
}

flow "throttled" {
  from {
    connector = "api"
    operation = "POST /work"
  }

  semaphore {
    storage {
      driver = "memory"
    }
    key   = "'api_quota'"
    `+written.body+`
    timeout = "5s"
  }

  response {
    ok = "true"
  }
}
`)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(config.Flows) != 1 || config.Flows[0].Semaphore == nil {
				t.Fatal("no semaphore parsed")
			}
			if config.Flows[0].Semaphore.MaxPermits != 10 {
				t.Errorf("permits = %d, want 10", config.Flows[0].Semaphore.MaxPermits)
			}
		})
	}
}

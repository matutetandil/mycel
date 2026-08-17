package runtime

import (
	"context"
	"testing"
)

// Whether a service limits how fast it is called, and where it keeps the count.
//
// The second question is the one that matters in a deployment: a limit kept in
// memory is per replica, so three replicas behind a load balancer let three
// times the configured rate through — which is not what anybody wrote down.

const rateLimitedService = `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9396

  rate_limit {
    enabled             = true
    requests_per_second = 50
    burst               = 100
    storage             = "cache"
  }
}

connector "api" {
  type = "rest"
  port = 3396
}

connector "cache" {
  type   = "cache"
  driver = "memory"
}
`

func TestAConfiguredLimitIsTheOneApplied(t *testing.T) {
	rt := newCheckRuntime(t, rateLimitedService)

	rt.initRateLimiter()

	if rt.rateLimiter == nil {
		t.Fatal("a service that configured a rate limit has none")
	}
}

func TestALimitTurnedOffIsNotApplied(t *testing.T) {
	// Writing the block and turning it off is how somebody keeps the settings
	// around without them being in force.
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9395

  rate_limit {
    enabled             = false
    requests_per_second = 50
  }
}

connector "api" {
  type = "rest"
  port = 3395
}
`)

	rt.initRateLimiter()

	if rt.rateLimiter != nil {
		t.Error("a rate limit that was turned off is in force")
	}
}

func TestTheEnvironmentDecidesWhenNothingIsConfigured(t *testing.T) {
	// Production defends itself by default; development does not, because a
	// limit nobody asked for looks like a bug while somebody is building.
	const plain = `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9394
}

connector "api" {
  type = "rest"
  port = 3394
}
`
	for environment, limited := range map[string]bool{
		"production":  true,
		"development": false,
	} {
		t.Run(environment, func(t *testing.T) {
			rt := newCheckRuntime(t, plain)
			rt.environment = environment

			rt.initRateLimiter()

			if (rt.rateLimiter != nil) != limited {
				t.Errorf("rate limited = %v, want %v for %s", rt.rateLimiter != nil, limited, environment)
			}
		})
	}
}

func TestALimitSharedAcrossReplicasNeedsSomewhereToShareIt(t *testing.T) {
	// The storage attribute names a connector, and every way of getting it
	// wrong has to leave the service running with its own count rather than
	// failing to start — a rate limit is a guard rail, not the road.
	rt := newCheckRuntime(t, rateLimitedService)
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}
	rt.initRateLimiter()

	// A memory cache provides no Redis client, so the limit stays per replica
	// and the service keeps serving.
	rt.upgradeRateLimiterToRedis()
	if rt.rateLimiter == nil {
		t.Error("the service lost its rate limiter to a storage it could not use")
	}
}

func TestStorageNamingAConnectorNobodyDeclaredIsSurvived(t *testing.T) {
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9393

  rate_limit {
    enabled             = true
    requests_per_second = 50
    storage             = "a_connector_nobody_declared"
  }
}

connector "api" {
  type = "rest"
  port = 3393
}
`)
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}
	rt.initRateLimiter()

	rt.upgradeRateLimiterToRedis()

	if rt.rateLimiter == nil {
		t.Error("a storage connector that does not exist took the rate limiter with it")
	}
}

func TestNoLimiterMeansNothingToUpgrade(t *testing.T) {
	// Called on every start, including services with no rate limit at all.
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9392
}

connector "api" {
  type = "rest"
  port = 3392
}
`)
	rt.upgradeRateLimiterToRedis()
}

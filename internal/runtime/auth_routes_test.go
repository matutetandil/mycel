package runtime

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Where the auth endpoints are reachable from.
//
// The handler existed and nothing ever called RegisterRoutes, so /auth/login,
// /auth/register and the rest — documented, configurable, and on by default —
// answered 404 on a running service.

const authedService = `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9391
}

connector "api" {
  type = "rest"
  port = 3391
}

connector "admin_api" {
  type = "rest"
  port = 3390
}

auth {
  preset = "development"

  jwt {
    secret = "a-secret-long-enough-for-the-tests"
  }
}
`

func TestAuthEndpointsAreMountedOnEveryFrontDoor(t *testing.T) {
	// A service with two REST servers gets them on both: either is a
	// legitimate way in, and mounting on one leaves callers of the other with
	// a 404 for an endpoint the configuration says exists.
	rt := newCheckRuntime(t, authedService)
	ctx := context.Background()

	if err := rt.initConnectors(ctx); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}
	if err := rt.initAuth(ctx); err != nil {
		t.Fatalf("initAuth: %v", err)
	}

	rt.mountAuthEndpoints()

	// Both servers answer the sign-in endpoint. The answer is a refusal, since
	// no credentials were sent — which is the point: the route exists.
	for name, port := range map[string]string{"api": "3391", "admin_api": "3390"} {
		conn, err := rt.connectors.Get(name)
		if err != nil {
			t.Fatalf("Get(%s): %v", name, err)
		}
		starter, ok := conn.(interface{ Start(context.Context) error })
		if !ok {
			t.Fatalf("%s does not start", name)
		}
		if err := starter.Start(ctx); err != nil {
			t.Fatalf("Start(%s): %v", name, err)
		}

		// The server binds on its own goroutine, so this waits for it rather
		// than assuming it is listening the moment Start returns.
		status := 0
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Post("http://127.0.0.1:"+port+"/auth/login",
				"application/json", strings.NewReader(`{}`))
			if err == nil {
				status = resp.StatusCode
				_ = resp.Body.Close()
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		if status == 0 {
			t.Fatalf("%s never started listening", name)
		}
		if status == http.StatusNotFound {
			t.Errorf("%s answers 404 for an endpoint the configuration says exists", name)
		}
	}
}

func TestMountingWithNoAuthConfiguredDoesNothing(t *testing.T) {
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9389
}

connector "api" {
  type = "rest"
  port = 3389
}
`)
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}

	// No auth block, so nothing to mount and nothing to complain about.
	rt.mountAuthEndpoints()
}

func TestAuthWithNowhereToBeReachedIsReported(t *testing.T) {
	// A service that configures accounts and has no HTTP server has endpoints
	// nobody can call, which is worth saying at startup.
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9388
}

connector "store" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

auth {
  preset = "development"

  jwt {
    secret = "a-secret-long-enough-for-the-tests"
  }
}
`)
	ctx := context.Background()
	if err := rt.initConnectors(ctx); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}
	if err := rt.initAuth(ctx); err != nil {
		t.Fatalf("initAuth: %v", err)
	}

	rt.mountAuthEndpoints()
}

// --- What a debugger can ask the runtime to do ------------------------------

func TestTheRuntimeListsWhatCanBeSteppedThrough(t *testing.T) {
	// A debugger shows the event-driven sources a message can be pulled
	// through one at a time. A source missing from this list is one nobody can
	// step through.
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9387
}

connector "api" {
  type = "rest"
  port = 3387
}
`)
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}

	// A REST server is not event-driven: a request arrives when a caller makes
	// one, so there is nothing to release.
	for _, source := range rt.ListEventSources() {
		if source.Connector == "api" {
			t.Error("a REST server was offered as something to step messages through")
		}
	}

	if err := rt.ConsumeOne(context.Background(), "api"); err == nil {
		t.Error("a message was released from a connector that does not hold any")
	}
	if err := rt.ConsumeOne(context.Background(), "a_connector_nobody_declared"); err == nil {
		t.Error("a message was released from a connector that does not exist")
	}
}

func TestTheRuntimeAlwaysHasATransformer(t *testing.T) {
	// A debugger evaluates expressions against a running service, and there is
	// no useful answer to "there is no transformer".
	rt := newCheckRuntime(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9386
}

connector "api" {
  type = "rest"
  port = 3386
}
`)

	if rt.GetCELTransformer() == nil {
		t.Error("the runtime cannot evaluate an expression")
	}

	// The debug server is absent unless one was started, and asking must not
	// panic.
	_ = rt.GetDebugServer()
}

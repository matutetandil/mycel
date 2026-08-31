package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A reload registers every connector again. Register used to append
// unconditionally, so each reload left behind a checker pointing at a
// connector the reload had already abandoned and closed. Those reported
// "sql: database is closed" for ever, and overall status is an AND across
// components — so one reload was enough to mark a service unhealthy
// permanently while it went on serving every request correctly.
//
// Everything downstream of a health endpoint acts on that: a container
// HEALTHCHECK, a Compose service_healthy condition, a Kubernetes readiness
// probe. None of them recover on their own, and none of them are wrong to
// trust the endpoint.

func componentsOf(t *testing.T, m *Manager) []Status {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	answer := httptest.NewRecorder()
	m.HealthHandler()(answer, request)

	var response Response
	if err := json.NewDecoder(answer.Body).Decode(&response); err != nil {
		t.Fatalf("decoding the health response: %v", err)
	}
	return response.Components
}

func statusOf(t *testing.T, m *Manager) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	answer := httptest.NewRecorder()
	m.HealthHandler()(answer, request)

	var response Response
	if err := json.NewDecoder(answer.Body).Decode(&response); err != nil {
		t.Fatalf("decoding the health response: %v", err)
	}
	return response.Status
}

func TestReloadingDoesNotLeaveTheOldCheckersBehind(t *testing.T) {
	m := NewManager("test")
	m.Register(&mockChecker{name: "api"})
	m.Register(&mockChecker{name: "db"})

	// Three reloads. Each hands over a fresh object for the same connector,
	// while the one it replaces has been closed.
	for reload := 0; reload < 3; reload++ {
		m.Register(&mockChecker{name: "api"})
		m.Register(&mockChecker{name: "db"})

		components := componentsOf(t, m)
		if len(components) != 2 {
			t.Fatalf("after %d reload(s) there are %d components, want 2: %v",
				reload+1, len(components), components)
		}
		if got := statusOf(t, m); got != "healthy" {
			t.Errorf("after %d reload(s) the service reports %q while serving normally",
				reload+1, got)
		}
	}
}

func TestAClosedConnectorDoesNotGoOnBeingChecked(t *testing.T) {
	m := NewManager("test")
	m.Register(&mockChecker{name: "api"})
	m.Register(&mockChecker{name: "db", err: errors.New("sql: database is closed")})

	if got := statusOf(t, m); got != "unhealthy" {
		t.Fatalf("a genuinely failing component did not make the service unhealthy: %q", got)
	}

	// The reload hands over a working replacement under the same name. The
	// old one is gone, and so is what it was reporting.
	m.Register(&mockChecker{name: "db"})

	if got := statusOf(t, m); got != "healthy" {
		t.Errorf("a replaced connector went on reporting the failure of the one it replaced: %q", got)
	}
	if components := componentsOf(t, m); len(components) != 2 {
		t.Errorf("%d components, want 2: %v", len(components), components)
	}
}

func TestAConnectorDroppedFromTheConfigurationStopsBeingChecked(t *testing.T) {
	// Registering by name stops the accumulation but cannot notice a checker
	// that should no longer exist at all: a reload that drops a connector
	// from the configuration left its checker behind, still pointing at the
	// object the reload closed, reporting for ever exactly as before. The
	// set is stated rather than added to.
	m := NewManager("test")
	m.SetCheckers([]Checker{
		&mockChecker{name: "api"},
		&mockChecker{name: "db"},
	})

	if components := componentsOf(t, m); len(components) != 2 {
		t.Fatalf("%d components, want 2", len(components))
	}

	// The reload drops "db". What it left behind would report
	// "sql: database is closed" for the life of the process.
	m.SetCheckers([]Checker{&mockChecker{name: "api"}})

	components := componentsOf(t, m)
	if len(components) != 1 {
		t.Fatalf("%d components after a connector was dropped, want 1: %v", len(components), components)
	}
	if components[0].Name != "api" {
		t.Errorf("the surviving component is %q", components[0].Name)
	}
	if got := statusOf(t, m); got != "healthy" {
		t.Errorf("dropping a connector left the service reporting %q", got)
	}
}

func TestStatingTheCheckersTwiceIsNotCumulative(t *testing.T) {
	m := NewManager("test")
	set := []Checker{&mockChecker{name: "api"}, &mockChecker{name: "db"}}

	m.SetCheckers(set)
	m.SetCheckers(set)
	m.SetCheckers(set)

	if components := componentsOf(t, m); len(components) != 2 {
		t.Errorf("%d components after stating the same set three times, want 2: %v",
			len(components), components)
	}

	// And a name registered afterwards still replaces rather than adds.
	m.Register(&mockChecker{name: "db"})
	if components := componentsOf(t, m); len(components) != 2 {
		t.Errorf("%d components, want 2: %v", len(components), components)
	}
}

func TestCheckersSurviveBeingUsedConcurrently(t *testing.T) {
	// A reload registers while requests are being served.
	m := NewManager("test")
	m.SetCheckers([]Checker{&mockChecker{name: "api"}, &mockChecker{name: "db"}})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			m.SetCheckers([]Checker{&mockChecker{name: "api"}, &mockChecker{name: "db"}})
			m.Register(&mockChecker{name: "db"})
		}
	}()

	for i := 0; i < 50; i++ {
		_ = m.checkAll(context.Background())
	}
	<-done

	if components := componentsOf(t, m); len(components) != 2 {
		t.Errorf("%d components, want 2: %v", len(components), components)
	}
}

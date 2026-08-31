package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What a reload leaves behind in the health manager.
//
// Every connector is registered again on every reload, and nothing removed
// the previous set, so each reload left a checker pointing at a connector the
// reload had already abandoned and closed. Those report "sql: database is
// closed" for ever, and overall status is an AND across components — so one
// reload was enough to mark the service unhealthy permanently while it went
// on serving every request correctly.
//
// The check is here rather than only in internal/health because the defect is
// in what the reload path does with the manager, not in the manager alone: a
// loop that calls Register once per connector passes every test in that
// package and still accumulates.

func healthAfterReloads(t *testing.T, r *Runtime) (string, []string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	answer := httptest.NewRecorder()
	r.health.HealthHandler()(answer, request)

	var response struct {
		Status     string `json:"status"`
		Components []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"components"`
	}
	if err := json.NewDecoder(answer.Body).Decode(&response); err != nil {
		t.Fatalf("decoding the health response: %v", err)
	}

	named := make([]string, 0, len(response.Components))
	for _, component := range response.Components {
		named = append(named, component.Name+":"+component.Status+" "+component.Message)
	}
	return response.Status, named
}

func TestReloadsDoNotAccumulateHealthCheckers(t *testing.T) {
	r, _ := reloadableRuntime(t, workingConfig)

	// The configuration has two connectors, so a service that reloads a
	// hundred times still has two components.
	for i := 1; i <= 3; i++ {
		if err := r.hotReloadSwitch(context.Background()); err != nil {
			t.Fatalf("reload %d: %v", i, err)
		}

		status, components := healthAfterReloads(t, r)
		if len(components) != 2 {
			t.Fatalf("after %d reload(s) health reports %d components, want 2: %v",
				i, len(components), components)
		}
		if status != "healthy" {
			t.Errorf("after %d reload(s) the service reports %q while serving normally: %v",
				i, status, components)
		}
	}
}

func TestAConnectorARemovedByAReloadStopsBeingChecked(t *testing.T) {
	// Registering by name is not enough on its own: this one is not replaced
	// by anything, so what is left behind points at an object the reload
	// closed and answers for the rest of the process's life.
	r, path := reloadableRuntime(t, workingConfig+`
connector "archive" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
`)
	if err := r.hotReloadSwitch(context.Background()); err != nil {
		t.Fatalf("hotReloadSwitch: %v", err)
	}
	if _, components := healthAfterReloads(t, r); len(components) != 3 {
		t.Fatalf("%d components before the connector was removed, want 3: %v", len(components), components)
	}

	rewrite(t, path, workingConfig)
	if err := r.hotReloadSwitch(context.Background()); err != nil {
		t.Fatalf("hotReloadSwitch after removing a connector: %v", err)
	}

	status, components := healthAfterReloads(t, r)
	if len(components) != 2 {
		t.Errorf("%d components after a connector was removed, want 2: %v", len(components), components)
	}
	for _, component := range components {
		if strings.HasPrefix(component, "archive:") {
			t.Errorf("a connector the configuration no longer declares is still being checked: %s", component)
		}
	}
	if status != "healthy" {
		t.Errorf("removing a connector left the service reporting %q: %v", status, components)
	}
}

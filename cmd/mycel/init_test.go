package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scaffold's whole job is to hand someone a working starting point. A
// layout that does not parse teaches the wrong first lesson, so the generated
// files are checked for the shape the runtime and the docs both expect.
func TestRunInit_WritesTheRecommendedLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "orders-service")

	if err := runInit(nil, []string{dir}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	for _, want := range []string{
		"config.mycel",
		"connectors/api.mycel",
		"flows/status.mycel",
		".gitignore",
		".env.example",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("expected %s to be created: %v", want, err)
		}
	}

	// One declaration per file, grouped by kind — the point of the layout.
	connectors, _ := os.ReadFile(filepath.Join(dir, "connectors/api.mycel"))
	if strings.Contains(string(connectors), "flow ") {
		t.Error("the connectors file should not declare flows")
	}
	flows, _ := os.ReadFile(filepath.Join(dir, "flows/status.mycel"))
	if strings.Contains(string(flows), "connector \"") {
		t.Error("the flows file should not declare connectors")
	}
}

// The service name reaches logs, metric labels and health output, so it comes
// from the directory rather than being left as a placeholder.
func TestRunInit_NamesTheServiceAfterTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "orders-service")

	if err := runInit(nil, []string{dir}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, "config.mycel"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(cfg), `name    = "orders-service"`) {
		t.Errorf("service name not taken from the directory:\n%s", cfg)
	}

	// The same name is echoed by the generated flow, so a running scaffold
	// identifies itself.
	flow, _ := os.ReadFile(filepath.Join(dir, "flows/status.mycel"))
	if !strings.Contains(string(flow), "orders-service") {
		t.Errorf("generated flow does not carry the service name:\n%s", flow)
	}
}

// Scaffolding is a starting move. Silently replacing a file someone has
// already edited is never what they meant.
func TestRunInit_RefusesToOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "svc")
	if err := runInit(nil, []string{dir}); err != nil {
		t.Fatalf("first runInit: %v", err)
	}

	sentinel := "// edited by hand\n"
	target := filepath.Join(dir, "flows/status.mycel")
	if err := os.WriteFile(target, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("writing sentinel: %v", err)
	}

	err := runInit(nil, []string{dir})
	if err == nil {
		t.Fatal("expected the second runInit to refuse")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error should say what it refused to do, got: %v", err)
	}

	// Nothing may be touched when it refuses — not even files that would not
	// themselves have clashed.
	got, _ := os.ReadFile(target)
	if string(got) != sentinel {
		t.Error("the hand-edited file was overwritten")
	}
}

func TestServiceName(t *testing.T) {
	cases := map[string]string{
		"orders-service": "orders-service",
		"my service":     "my-service",
		"":               "my-service",
		".":              "my-service",
	}
	for in, want := range cases {
		if got := serviceName(in); got != want {
			t.Errorf("serviceName(%q) = %q, want %q", in, got, want)
		}
	}
}

package docs

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// render runs `helm template` over the chart with the given --set arguments.
func render(t *testing.T, sets ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	args := []string{"template", "test", "../../helm/mycel", "--set", "metrics.serviceMonitor.enabled=true"}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

// The chart has to probe a port the service it installs actually serves.
//
// It probed /health/live and /health/ready on the app port, which only exists
// if a connector is listening on it — and the chart's own default
// configuration declares no connector at all. So `helm install mycel` with the
// defaults produced a pod whose readiness never passed and whose liveness
// probe restarted it: the recommended way onto Kubernetes did not start.
// Health lives on the admin server, which is up whatever the connectors do.
func TestTheChartProbesThePortItServes(t *testing.T) {
	manifests := render(t)

	if !strings.Contains(manifests, "name: admin") {
		t.Fatal("no container port named admin in the rendered manifests")
	}

	probe := regexp.MustCompile(`(?s)(liveness|readiness)Probe:.*?port: (\w+)`)
	found := probe.FindAllStringSubmatch(manifests, -1)
	if len(found) != 2 {
		t.Fatalf("expected a liveness and a readiness probe, found %d", len(found))
	}
	for _, match := range found {
		if match[2] != "admin" {
			t.Errorf("%sProbe hits port %q; health is served on the admin port", match[1], match[2])
		}
	}
}

// /metrics is served by the admin server too, so that is where a scraper has
// to look. The ServiceMonitor pointed at the app port, which serves flows.
func TestTheServiceMonitorScrapesTheAdminPort(t *testing.T) {
	manifests := render(t)

	monitor := manifests[strings.Index(manifests, "kind: ServiceMonitor"):]
	endpoint := regexp.MustCompile(`endpoints:\s*\n\s*- port: (\w+)`).FindStringSubmatch(monitor)
	if endpoint == nil {
		t.Fatal("the ServiceMonitor declares no endpoint port")
	}
	if endpoint[1] != "admin" {
		t.Errorf("the ServiceMonitor scrapes %q; /metrics is on the admin port", endpoint[1])
	}
}

// Changing service.adminPort has to move the port everywhere at once: the
// container port, the probes' target, and the admin_port the configuration
// tells the runtime to listen on. If those drift the pod probes a port nothing
// is bound to, which is the failure this whole area already had once.
func TestChangingTheAdminPortMovesTheConfigWithIt(t *testing.T) {
	manifests := render(t, "service.adminPort=9800")

	if !strings.Contains(manifests, "containerPort: 9800") {
		t.Error("the container port did not follow service.adminPort")
	}
	if !strings.Contains(manifests, "admin_port = 9800") {
		t.Error("the rendered configuration did not follow service.adminPort")
	}
}

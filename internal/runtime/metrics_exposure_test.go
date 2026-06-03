package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/metrics"
)

// TestFlowMetricsExposedOnAdminServer guards a production bug: on a service
// without a REST connector (e.g. an MQ consumer), flow metrics are recorded on
// every execution via metrics.Default() (flow_registry.go), yet `mycel_flow_*`
// never appeared at /metrics.
//
// Root cause: Default() lazy-initialised through a sync.Once that clobbered the
// registry SetDefault had assigned — the first Default() call replaced the
// runtime's registry with a throwaway one nothing serves. This test records the
// way the flow handler does and asserts the series is exposed on the admin endpoint.
func TestFlowMetricsExposedOnAdminServer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-metrics-repro-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	const port = 19097
	configHCL := `
service {
  name    = "metrics-repro"
  version = "1.0.0"
  admin_port = 19097
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(configHCL), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Shutdown()
	waitForServer(t, port)

	// Record a flow execution the same way FlowHandler.HandleRequest does:
	// through the package-level Default() registry.
	metrics.Default().RecordFlowExecution("repro_flow", "success", 5*time.Millisecond)

	body := scrapeText(t, fmt.Sprintf("http://localhost:%d/metrics", port))
	if !strings.Contains(body, "mycel_flow_executions_total") {
		t.Fatalf("flow metrics recorded via metrics.Default() are NOT exposed on the admin /metrics "+
			"(metrics.Default() must be the same registry the server serves). mycel_* families present: %s",
			familiesPresent(body))
	}
}

func scrapeText(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// familiesPresent lists the mycel_* metric family names in the body, for a
// readable failure message.
func familiesPresent(body string) string {
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "mycel_") {
			name := line
			if i := strings.IndexAny(name, "{ "); i >= 0 {
				name = name[:i]
			}
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

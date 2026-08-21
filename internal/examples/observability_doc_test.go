package examples

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The observability guide prints what the health endpoints answer with, and
// somebody writes a probe or a dashboard against it.
//
// Its samples had drifted: `/health` reports two versions — Mycel's and the
// one the service block declares — where the page showed one, and `/health/ready`
// carries the components, which is what makes a service ready in the first
// place. A field that is on the page and not in the answer is a probe reading
// nothing; the reverse is a field nobody knows is there.
func TestTheHealthEndpointsAnswerWithWhatTheGuideShows(t *testing.T) {
	if testing.Short() {
		t.Skip("starting a service")
	}

	page, err := os.ReadFile(repoPath("docs", "guides", "observability.md"))
	if err != nil {
		t.Fatalf("reading the guide: %v", err)
	}

	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.mycel", "service {\n  name    = \"probe-demo\"\n  version = \"1.0.0\"\n}\n")
	write("connectors.mycel", "connector \"api\" {\n  type = \"rest\"\n  port = 3000\n}\n")
	write("flows.mycel", "flow \"ping\" {\n  from {\n    connector = \"api\"\n    operation = \"GET /ping\"\n  }\n  response {\n    ok = \"true\"\n  }\n}\n")

	svc := startDir(t, dir, "observability guide")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the REST port was not moved")
	}

	for _, endpoint := range []struct {
		path  string
		after string // the heading the sample follows, so the right block is compared
	}{
		{"/health", "### Detailed Health"},
		{"/health/live", "### Liveness Probe"},
		{"/health/ready", "### Readiness Probe"},
	} {
		status, answer := svc.run(t, "curl http://localhost:"+strconv.Itoa(port)+endpoint.path)
		if status != 200 {
			t.Errorf("%s answered %d", endpoint.path, status)
			continue
		}

		var got map[string]interface{}
		if err := json.Unmarshal([]byte(answer), &got); err != nil {
			t.Errorf("%s did not answer JSON: %v", endpoint.path, err)
			continue
		}

		shown := documentedShape(t, string(page), endpoint.after)
		if shown == nil {
			t.Errorf("the guide no longer shows what %s answers", endpoint.path)
			continue
		}

		for field := range shown {
			if _, present := got[field]; !present {
				t.Errorf("%s: the guide shows %q and the answer has no such field", endpoint.path, field)
			}
		}
		for field := range got {
			if _, documented := shown[field]; !documented {
				t.Errorf("%s answers with %q, which the guide does not show", endpoint.path, field)
			}
		}
	}
}

var healthySample = regexp.MustCompile("(?s)```json\\n(\\{.*?\\})\\n```")

// documentedShape returns the first healthy JSON sample after a heading.
func documentedShape(t *testing.T, page, heading string) map[string]interface{} {
	t.Helper()

	from := strings.Index(page, heading)
	if from < 0 {
		return nil
	}
	rest := page[from:]
	if next := strings.Index(rest[len(heading):], "\n### "); next > 0 {
		rest = rest[:len(heading)+next]
	}

	for _, m := range healthySample.FindAllStringSubmatch(rest, -1) {
		var shape map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &shape); err != nil {
			continue
		}
		// The unhealthy sample is a different shape on purpose.
		if shape["status"] == "healthy" {
			return shape
		}
	}
	return nil
}

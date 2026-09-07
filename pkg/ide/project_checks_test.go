package ide

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/runtime"
	"github.com/matutetandil/mycel/v3/pkg/connectors"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// The editor runs the checks `mycel validate` runs.
//
// It kept its own list, so a configuration validate refused could look clean
// in the editor: a cache key written as CEL (#100), `headers` aimed at a
// database (#103), `key` and `key_from` together (#104) — every one of them
// a squiggle-free file that failed the moment it was started. And every
// check added to the runtime had to be written a second time for the editor
// or it was simply absent there. Now the project is fed to the real parser
// and to runtime.Checks, and each finding lands on the block it names.

const projectConnectors = `
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

connector "redis" {
  type   = "cache"
  driver = "memory"
}
`

func withMessage(diags []*Diagnostic, want string) *Diagnostic {
	for _, d := range diags {
		if strings.Contains(d.Message, want) {
			return d
		}
	}
	return nil
}

func TestACacheKeyWrittenAsCELIsFlaggedInTheEditor(t *testing.T) {
	e, dir := project(t, map[string]string{
		"connectors.mycel": projectConnectors,
		"flows.mycel": `
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  cache {
    storage = "redis"
    ttl     = "5m"
    key     = "'product:' + input.id"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
`})
	flows := filepath.Join(dir, "flows.mycel")

	d := withMessage(e.Diagnose(flows), "template")
	if d == nil {
		t.Fatalf("the CEL cache key validate refuses drew nothing in the editor: %v", messages(e.Diagnose(flows)))
	}
	if d.File != flows || d.Range.Start.Line != 2 {
		t.Errorf("the finding is not on the flow's block: file=%s line=%d", d.File, d.Range.Start.Line)
	}
	if d.Severity != SeverityError {
		t.Errorf("severity = %v, want an error like validate's", d.Severity)
	}
	// The other file has nothing to answer for.
	if got := withMessage(e.Diagnose(filepath.Join(dir, "connectors.mycel")), "template"); got != nil {
		t.Errorf("the finding was also reported on the connectors file")
	}
}

func TestHeadersAimedAtADatabaseAreFlaggedInTheEditor(t *testing.T) {
	e, dir := project(t, map[string]string{
		"connectors.mycel": projectConnectors,
		"flows.mycel": `
flow "lookup" {
  from {
    connector = "api"
    operation = "GET /lookup"
  }
  step "rows" {
    connector = "db"
    query     = "SELECT 1 AS one"
    headers   = { Store = "input.store" }
  }
  to {
    connector = "db"
    target    = "audit"
  }
}
`})
	d := withMessage(e.Diagnose(filepath.Join(dir, "flows.mycel")), "sends no request headers")
	if d == nil {
		t.Fatal("headers aimed at a database drew nothing in the editor")
	}
	if !strings.Contains(d.Message, `step "rows"`) {
		t.Errorf("the finding does not name the step: %s", d.Message)
	}
}

func TestASemanticParseErrorIsFlaggedInTheEditor(t *testing.T) {
	// The editor's own HCL indexer accepts anything syntactically well formed,
	// so a refusal the Mycel parser makes — both key and key_from — never
	// reached it.
	e, dir := project(t, map[string]string{
		"connectors.mycel": projectConnectors,
		"flows.mycel": `
flow "gallery" {
  from {
    connector = "api"
    operation = "GET /gallery"
  }
  cache {
    storage  = "redis"
    key      = "gallery:${input.store}"
    key_from = "'gallery:' + input.store"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`})
	flows := filepath.Join(dir, "flows.mycel")
	d := withMessage(e.Diagnose(flows), "both key and key_from")
	if d == nil {
		t.Fatalf("a block with key and key_from drew nothing in the editor: %v", messages(e.Diagnose(flows)))
	}
	if d.File != flows || d.Range.Start.Line != 2 {
		t.Errorf("the finding is not on the flow's block: file=%s line=%d", d.File, d.Range.Start.Line)
	}
}

func TestACleanProjectHasNoProjectFindings(t *testing.T) {
	e, dir := project(t, map[string]string{
		"connectors.mycel": projectConnectors,
		"flows.mycel": `
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  cache {
    storage = "redis"
    ttl     = "5m"
    key     = "product:${input.id}"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
`})
	if diags := e.DiagnoseAll(); len(diags) != 0 {
		t.Errorf("a clean project was flagged: %v", messages(diags))
	}
	_ = dir
}

func TestTheUnsavedBufferIsWhatGetsChecked(t *testing.T) {
	e, dir := project(t, map[string]string{
		"connectors.mycel": projectConnectors,
		"flows.mycel": `
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
`})
	flows := filepath.Join(dir, "flows.mycel")

	// The file on disk is fine; the editor's buffer is not.
	diags := e.UpdateFile(flows, []byte(`
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  cache {
    storage = "redis"
    key     = "'product:' + input.id"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
`))
	if withMessage(diags, "template") == nil {
		t.Errorf("the unsaved buffer's mistake was not reported; the disk copy was checked instead: %v", messages(diags))
	}
}

func TestAFileStillBeingTypedSuspendsTheProjectChecks(t *testing.T) {
	// Half a block is a syntax error the editor already reports; running the
	// whole-project checks over it would add a second, misleading finding
	// about a configuration that does not exist yet.
	e, dir := project(t, map[string]string{
		"connectors.mycel": projectConnectors,
		"flows.mycel":      "flow \"half\" {\n  from {\n",
	})
	diags := e.Diagnose(filepath.Join(dir, "flows.mycel"))
	for _, d := range diags {
		if strings.Contains(d.Message, "failed to parse") || strings.Contains(d.Message, "parse error") {
			t.Errorf("a project-level parse finding was reported on a file being typed: %s", d.Message)
		}
	}
}

// The two lists are tied together: a check added to the runtime is either
// surfaced by the editor through this layer, or named here as one the editor
// already reports with a precise range of its own.
func TestEveryRuntimeCheckReachesTheEditor(t *testing.T) {
	reg := schema.NewRegistryWith(connectors.RegisterAll)
	kinds := map[string]bool{}
	for _, check := range runtime.Checks(nil, reg) {
		kinds[check.Kind] = true
	}
	for kind := range coveredPositionally {
		if !kinds[kind] {
			t.Errorf("coveredPositionally names %q, which is no longer a runtime check kind", kind)
		}
	}
	surfaced := 0
	for kind := range kinds {
		if !coveredPositionally[kind] {
			surfaced++
		}
	}
	// Written down so that a new runtime check makes someone decide whether
	// the editor already has it, rather than the answer being silently "yes".
	if surfaced != 8 {
		t.Errorf("%d runtime check kinds reach the editor through the project layer, and this test knows about 8 — "+
			"a new check was added: leave it to the project layer and update this number, or add it to coveredPositionally if the editor already reports it", surfaced)
	}
}

func messages(diags []*Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Message)
	}
	return out
}

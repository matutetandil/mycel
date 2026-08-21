package ide

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// What an editor does while somebody types: the file changes, the index has to
// follow, and the answers have to match the buffer rather than what is on disk.
// An index that lags is worse than none — it reports a connector as undefined
// while the author is looking at the line that defines it.

func project(t *testing.T, files map[string]string) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	e := NewEngine(dir)
	e.FullReindex()
	return e, dir
}

const connectorsFile = `
connector "api" {
  type = "rest"
  port = 3000
}
`

const flowsFile = `
flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }
}
`

func TestAnUnsavedEditIsWhatTheAnswersComeFrom(t *testing.T) {
	// The buffer, not the file: an editor asks about what is on screen.
	e, dir := project(t, map[string]string{
		"connectors.mycel": connectorsFile,
		"flows.mycel":      flowsFile,
	})
	flows := filepath.Join(dir, "flows.mycel")

	// Nothing wrong yet.
	if diags := e.Diagnose(flows); len(diags) != 0 {
		t.Fatalf("diagnostics on a file that is fine: %v", diags)
	}

	// The author renames the connector in the buffer, without saving.
	e.UpdateFile(flows, []byte(`
flow "list_orders" {
  from {
    connector = "a_connector_nobody_declared"
    operation = "GET /orders"
  }
}
`))

	diags := e.Diagnose(flows)
	if len(diags) == 0 {
		t.Fatal("the edit in the buffer was not seen")
	}
	if !strings.Contains(diags[0].Message, "a_connector_nobody_declared") {
		t.Errorf("diagnostic = %q, want it to name what is missing", diags[0].Message)
	}
}

func TestAFileThatGoesAwayTakesItsDefinitionsWithIt(t *testing.T) {
	// Deleting the file that declares a connector has to leave the flows that
	// name it reported, or the editor keeps saying everything is fine.
	e, dir := project(t, map[string]string{
		"connectors.mycel": connectorsFile,
		"flows.mycel":      flowsFile,
	})

	diags := e.RemoveFile(filepath.Join(dir, "connectors.mycel"))

	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "api") {
			found = true
		}
	}
	if !found {
		t.Errorf("removing the file that declares the connector reported nothing: %v", diags)
	}
}

func TestAskingAboutAFileNobodyOpenedAnswersNothing(t *testing.T) {
	// Rather than a panic on a path the index has never seen, which is what an
	// editor sends the moment it opens a file outside the project.
	e, _ := project(t, map[string]string{"connectors.mycel": connectorsFile})

	if items := e.Complete("/somewhere/else.mycel", 1, 1); items != nil {
		t.Errorf("completions = %v, want none", items)
	}
	if loc := e.Definition("/somewhere/else.mycel", 1, 1); loc != nil {
		t.Errorf("definition = %v, want none", loc)
	}
	if hover := e.Hover("/somewhere/else.mycel", 1, 1); hover != nil {
		t.Errorf("hover = %v, want none", hover)
	}
	if diags := e.Diagnose("/somewhere/else.mycel"); len(diags) != 0 {
		t.Errorf("diagnostics = %v, want none", diags)
	}
}

func TestCompletingInsideABlockOffersWhatBelongsThere(t *testing.T) {
	// The whole point: what is offered has to come from the schema, so a
	// connector's own attributes appear and nothing else does.
	e, dir := project(t, map[string]string{"flows.mycel": `
flow "list_orders" {
  from {
    
  }
}
`})

	items := e.Complete(filepath.Join(dir, "flows.mycel"), 4, 5)
	if len(items) == 0 {
		t.Fatal("nothing was offered inside a from block")
	}

	offered := map[string]bool{}
	for _, item := range items {
		offered[item.Label] = true
	}
	for _, want := range []string{"connector", "operation"} {
		if !offered[want] {
			t.Errorf("%q was not offered inside from: %v", want, offered)
		}
	}
	// Something belonging to another block is not offered here.
	if offered["target"] {
		t.Error("an attribute of the to block was offered inside from")
	}
}

func TestTheRegistryMakesCompletionsConnectorAware(t *testing.T) {
	// Without it the engine falls back to a static list, which is how a
	// connector's own settings stop being offered the day one is added.
	registry := schema.NewRegistry()
	e := NewEngine("", WithRegistry(registry))

	if e.Registry() != registry {
		t.Error("the registry handed in is not the one in use")
	}

	// And an engine without one still answers, rather than refusing to work.
	if NewEngine("").Registry() != nil {
		t.Error("an engine with no registry reports one")
	}
}

func TestRenamingAFileKeepsWhatItDeclared(t *testing.T) {
	// Moving connectors.mycel into a connectors/ directory must not make every
	// flow that names them report an undefined connector.
	e, dir := project(t, map[string]string{
		"connectors.mycel": connectorsFile,
		"flows.mycel":      flowsFile,
	})

	from := filepath.Join(dir, "connectors.mycel")
	to := filepath.Join(dir, "connectors", "api.mycel")

	e.RenameFile(from, to)

	if diags := e.Diagnose(filepath.Join(dir, "flows.mycel")); len(diags) != 0 {
		t.Errorf("moving the file broke the flows that name it: %v", diags)
	}
	if loc := e.Definition(filepath.Join(dir, "flows.mycel"), 4, 18); loc == nil || loc.File != to {
		t.Errorf("definition = %v, want the file's new path", loc)
	}
}

func TestReindexingSkipsWhatIsNotConfiguration(t *testing.T) {
	// A project has a node_modules or a plugin cache in it often enough that
	// walking them would be slow and would report diagnostics for files nobody
	// wrote.
	e, dir := project(t, map[string]string{
		"connectors.mycel":                 connectorsFile,
		"node_modules/thing/config.mycel":  "this does not parse {",
		"mycel_plugins/store/plugin.mycel": "neither does this {",
		".hidden/config.mycel":             "nor this {",
	})

	diags := e.FullReindex()
	for _, d := range diags {
		if strings.Contains(d.File, "node_modules") ||
			strings.Contains(d.File, "mycel_plugins") ||
			strings.Contains(d.File, ".hidden") {
			t.Errorf("a file outside the configuration was indexed: %s", d.File)
		}
	}
	_ = dir
}

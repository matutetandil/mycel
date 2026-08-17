package ide

import (
	"path/filepath"
	"strings"
	"testing"
)

// What an editor offers to do about a problem, and what it suggests about how a
// project is laid out. Both are the difference between a diagnostic somebody
// acts on and one they scroll past.

func TestAMissingConnectorCanBeCreatedFromTheDiagnostic(t *testing.T) {
	// The fix somebody wants is the one that writes the block: naming the
	// problem and leaving them to type it out is the part editors are for.
	e, dir := project(t, map[string]string{"flows.mycel": `
flow "list_orders" {
  from {
    connector = "orders_api"
    operation = "GET /orders"
  }
}
`})
	path := filepath.Join(dir, "flows.mycel")

	diags := e.Diagnose(path)
	if len(diags) == 0 {
		t.Fatal("a flow naming a connector nobody declared was reported as fine")
	}

	actions := e.CodeActions(path, diags[0].Range.Start.Line, diags[0].Range.Start.Col)
	if len(actions) == 0 {
		t.Fatal("nothing was offered for an undefined connector")
	}

	var creates CodeAction
	var found bool
	for _, action := range actions {
		if strings.Contains(action.Title, "orders_api") {
			creates = action
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no action names the connector: %v", actions)
	}
	if len(creates.Edits) == 0 {
		t.Fatal("the action changes nothing")
	}
	if !strings.Contains(creates.Edits[0].NewText, `connector "orders_api"`) {
		t.Errorf("the edit does not declare the connector:\n%s", creates.Edits[0].NewText)
	}
}

func TestAMissingRequiredAttributeCanBeFilledIn(t *testing.T) {
	e, dir := project(t, map[string]string{"flows.mycel": `
flow "list_orders" {
  from {
    operation = "GET /orders"
  }
}
`})
	path := filepath.Join(dir, "flows.mycel")

	diags := e.Diagnose(path)
	if len(diags) == 0 {
		t.Fatal("a from block missing its connector was reported as fine")
	}

	var offered bool
	for _, d := range diags {
		for _, action := range e.CodeActions(path, d.Range.Start.Line, d.Range.Start.Col) {
			if strings.Contains(action.Title, "connector") {
				offered = true
				if len(action.Edits) == 0 || !strings.Contains(action.Edits[0].NewText, "connector") {
					t.Errorf("the edit does not add the attribute: %+v", action.Edits)
				}
			}
		}
	}
	if !offered {
		t.Error("nothing was offered for a missing required attribute")
	}
}

func TestAFileHoldingSeveralThingsIsPointedOut(t *testing.T) {
	// One declaration per file is what the documentation recommends, and the
	// hint is how somebody finds that out without reading it.
	e, dir := project(t, map[string]string{"everything.mycel": `
connector "api" {
  type = "rest"
  port = 3000
}

connector "store" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
`})

	hints := e.HintsForFile(filepath.Join(dir, "everything.mycel"))
	if len(hints) == 0 {
		t.Fatal("a file holding two connectors drew no comment")
	}

	var mentioned bool
	for _, hint := range hints {
		if strings.Contains(hint.Message, "api") || strings.Contains(hint.Message, "store") ||
			strings.Contains(strings.ToLower(hint.Message), "split") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("the hints say nothing useful: %v", hints)
	}
}

func TestAFileNamedAfterSomethingElseIsPointedOut(t *testing.T) {
	// A file called validation.mycel declaring only flow "validated_create" is
	// the case this exists for — the name is the only clue where to look.
	e, dir := project(t, map[string]string{"validation.mycel": `
flow "validated_create" {
  from {
    connector = "api"
    operation = "POST /validate"
  }
}

connector "api" {
  type = "rest"
  port = 3000
}
`})

	hints := e.HintsForFile(filepath.Join(dir, "validation.mycel"))
	if len(hints) == 0 {
		t.Error("a file named after nothing it declares drew no comment")
	}
}

func TestHintsForAFileNobodyOpenedAreNone(t *testing.T) {
	e, _ := project(t, map[string]string{"connectors.mycel": connectorsFile})

	if hints := e.HintsForFile("/somewhere/else.mycel"); hints != nil {
		t.Errorf("hints = %v, want none", hints)
	}
}

func TestAProjectAllInOneDirectoryIsPointedOut(t *testing.T) {
	// Connectors, flows and types side by side in the root is what a project
	// looks like before anybody organises it.
	e, _ := project(t, map[string]string{
		"connectors.mycel": connectorsFile,
		"flows.mycel":      flowsFile,
		"types.mycel": `
type "Order" {
  id = id
}
`,
	})

	hints := e.Hints()
	if len(hints) == 0 {
		t.Error("a project with everything in one directory drew no comment")
	}
}

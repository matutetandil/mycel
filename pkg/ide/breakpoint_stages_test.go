package ide

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/dap"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// The gutter offers a place to stop; the runtime has to have one there.
//
// The editor built its own list of pipeline stages, the trace package declared
// another, and the debug adapter kept a third mapping stages to lines. All
// three disagreed: the editor offered `response`, which was not a stage at all,
// and stayed quiet about `read`, which is.

func TestEveryStageTheEditorOffersCanBeStoppedAt(t *testing.T) {
	e := NewEngine("")
	const config = `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "/tmp/x.db"
}

flow "everything" {
  from {
    connector = "db"
    filter    = "input.id != ''"
  }
  accept { when = "input.ok" }
  dedupe {
    cache = "db"
    key   = "input.id"
  }
  validate {
    input  = "thing"
    output = "thing"
  }
  enrich {
    connector = "db"
    as        = "extra"
  }
  step "one" {
    connector = "db"
  }
  transform {
    id = "input.id"
  }
  to {
    connector = "db"
    target    = "things"
  }
  response {
    id = "output.id"
  }
}`

	e.UpdateFile("flows/everything.mycel", []byte(strings.TrimSpace(config)))

	stages := e.FlowStages("everything")
	if len(stages) == 0 {
		t.Fatal("no stages offered for a flow that declares every block; this test is checking nothing")
	}

	for _, stage := range stages {
		if _, ok := dap.StageLines[stage]; !ok {
			t.Errorf("the editor offers a breakpoint at %q, and no stage of that name can be stopped at, "+
				"so setting one waits for something that never happens", stage)
		}
	}
}

func TestAReadingFlowStopsAtRead(t *testing.T) {
	// The destination stage is named for what the flow does there. A flow
	// serving GET was offered a breakpoint at "write", and the runtime records
	// "read" — so the one the gutter showed at the `to` block never fired.
	e := NewEngine("")
	const config = `
connector "api" {
  type = "rest"
  port = 8080
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "/tmp/x.db"
}

flow "list_things" {
  from {
    connector = "api"
    operation = "GET /things"
  }
  to {
    connector = "db"
    target    = "things"
  }
}

flow "make_thing" {
  from {
    connector = "api"
    operation = "POST /things"
  }
  to {
    connector = "db"
    target    = "things"
  }
}`
	e.UpdateFile("flows/things.mycel", []byte(strings.TrimSpace(config)))

	if got := e.FlowStages("list_things"); !hasStage(got, "read") || hasStage(got, "write") {
		t.Errorf("a flow serving GET offers %v; it stops at read, not write", got)
	}
	if got := e.FlowStages("make_thing"); !hasStage(got, "write") || hasStage(got, "read") {
		t.Errorf("a flow serving POST offers %v; it stops at write", got)
	}
}

func hasStage(stages []string, want string) bool {
	for _, s := range stages {
		if s == want {
			return true
		}
	}
	return false
}

func TestTheStagesAreOfferedInTheOrderTheyRun(t *testing.T) {
	e := NewEngine("")
	const config = `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "/tmp/x.db"
}

flow "everything" {
  from {
    connector = "db"
    filter    = "input.id != ''"
  }
  accept { when = "input.ok" }
  dedupe {
    cache = "db"
    key   = "input.id"
  }
  validate {
    input  = "thing"
    output = "thing"
  }
  enrich "extra" {
    connector = "db"
  }
  step "one" {
    connector = "db"
  }
  transform {
    id = "input.id"
  }
  to {
    connector = "db"
    target    = "things"
  }
  response {
    id = "output.id"
  }
}`
	e.UpdateFile("flows/everything.mycel", []byte(strings.TrimSpace(config)))

	stages := e.FlowStages("everything")
	last := -1
	for _, stage := range stages {
		order, known := schema.StageOrder(stage)
		if !known {
			t.Errorf("%q is offered and is not a pipeline stage", stage)
			continue
		}
		if order < last {
			t.Errorf("the stages are offered as %v, which is not the order they run in", stages)
			break
		}
		last = order
	}
}

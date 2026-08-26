package examples

import (
	"strings"
	"testing"
)

// Every port an example binds has to be moved, including the ones that are not
// a connector's.
//
// The workflow API is declared inside `service { workflow { api { port } } }`,
// and the rewriter only walked connector blocks — so the workflows example
// stayed on its declared 9091 while its README's requests were moved with
// everything else. Two examples could not run at once, and anything already
// holding 9091 turned the example into a thirty-second timeout, which is how
// this was found.
func TestTheWorkflowAPIPortIsMovedToo(t *testing.T) {
	source := `service {
  name = "orders"

  workflow {
    storage = "postgres"

    api {
      port = 9091

      auth {
        type = "api_key"
        keys = [env("WORKFLOW_API_KEY", "demo-key")]
      }
    }
  }
}

connector "api" {
  type = "rest"
  port = 3000
}
`

	moved := map[int]int{}
	rewritten, _ := movePorts(t, source, moved)

	to, ok := moved[9091]
	if !ok {
		t.Fatal("the workflow API port was left where it was declared")
	}
	if to == 9091 {
		t.Errorf("the workflow API port was 'moved' to itself")
	}
	if strings.Contains(rewritten, "port = 9091") {
		t.Errorf("9091 still appears in the rewritten config:\n%s", rewritten)
	}

	// The README's requests are moved by looking the declared port up in this
	// map, so recording it is the half that makes the commands follow.
	if _, ok := moved[3000]; !ok {
		t.Error("the REST port stopped being moved")
	}
}

// A port that is not inside an api block is none of this rule's business.
func TestOnlyTheAPIBlockPortIsTreatedAsTheWorkflowAPI(t *testing.T) {
	source := `connector "upstream" {
  type     = "http"
  base_url = "http://payments:8080"
  timeout  = 30
}
`
	moved := map[int]int{}
	rewritten, _ := movePorts(t, source, moved)

	if len(moved) != 0 {
		t.Errorf("moved %v, but nothing here is a port this service listens on", moved)
	}
	if rewritten != source {
		t.Errorf("config was rewritten:\n%s", rewritten)
	}
}

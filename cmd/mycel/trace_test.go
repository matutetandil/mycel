package main

import (
	"strings"
	"testing"
)

// `mycel trace` runs one flow, once, without starting any servers, and prints
// what happened at each stage. It is what somebody reaches for when a flow does
// the wrong thing and the logs of a running service are not enough — so it has
// to work against the same configuration the service would load, and say
// clearly when the flow they named is not there.

const traceService = `
service {
  name = "traced"
}

connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

flow "shape_a_customer" {
  from {
    connector = "api"
    operation = "POST /customers"
  }

  response {
    email    = "lower(input.email)"
    greeting = "'hello ' + input.name"
  }
}

flow "write_a_customer" {
  from {
    connector = "api"
    operation = "POST /customers/write"
  }
  to {
    connector = "db"
    target    = "customers"
  }
}
`

// tracing points the command at a project and restores the flags afterwards,
// since they are package-level and shared with every other test here.
func tracing(t *testing.T, files map[string]string) {
	t.Helper()
	dir := project(t, files)

	previousDir, previousInput, previousList := configDir, traceInput, traceList
	configDir = dir
	t.Cleanup(func() {
		configDir, traceInput, traceList = previousDir, previousInput, previousList
	})
}

func TestTraceListsTheFlowsItCanRun(t *testing.T) {
	tracing(t, map[string]string{"config.mycel": traceService})
	traceList = true

	if err := runTrace(nil, nil); err != nil {
		t.Fatalf("runTrace: %v", err)
	}
}

func TestTraceRunsTheFlowItWasGiven(t *testing.T) {
	tracing(t, map[string]string{"config.mycel": traceService})
	traceList = false
	traceInput = `{"email":"ADA@EXAMPLE.COM","name":"ada"}`

	if err := runTrace(nil, []string{"shape_a_customer"}); err != nil {
		t.Fatalf("runTrace: %v", err)
	}
}

func TestTraceSaysWhichFlowsExistWhenTheNameIsWrong(t *testing.T) {
	// The whole point of the message: somebody mistyped, and the answer should
	// save them a trip to the configuration file.
	tracing(t, map[string]string{"config.mycel": traceService})
	traceList = false
	traceInput = ""

	err := runTrace(nil, []string{"shape_a_custmoer"})
	if err == nil {
		t.Fatal("a flow nobody defined was traced")
	}
	if !strings.Contains(err.Error(), "shape_a_customer") {
		t.Errorf("error = %q, want it to list the flows that do exist", err)
	}
}

func TestTraceNeedsAFlowName(t *testing.T) {
	tracing(t, map[string]string{"config.mycel": traceService})
	traceList = false

	err := runTrace(nil, nil)
	if err == nil {
		t.Fatal("trace ran with no flow named")
	}
	if !strings.Contains(err.Error(), "--list") {
		t.Errorf("error = %q, want it to point at the way to find one", err)
	}
}

func TestTraceRefusesInputThatIsNotJSON(t *testing.T) {
	// Better than running the flow with an empty input and reporting whatever
	// that produces.
	tracing(t, map[string]string{"config.mycel": traceService})
	traceList = false
	traceInput = `{"email": "ada@example.com"`

	if err := runTrace(nil, []string{"shape_a_customer"}); err == nil {
		t.Error("input that is not JSON was accepted")
	}
}

func TestTraceReportsAConfigurationItCannotLoad(t *testing.T) {
	tracing(t, map[string]string{"config.mycel": `
service {
  name = "broken"
}

flow "orphan" {
  from {
    connector = "nobody"
    operation = "GET /x"
  }
}
`})
	traceList = true

	if err := runTrace(nil, nil); err == nil {
		t.Error("a configuration naming a connector that does not exist was traced")
	}
}

package parser

import (
	"strings"
	"testing"
)

// An aspect's action is the cross-cutting work itself — the audit row, the
// notification, the flow it hands off to. What it names decides whether that
// work happens at all, and an action naming nothing runs on every matching flow
// and does nothing.

func aspectFrom(t *testing.T, body string) *Configuration {
	t.Helper()
	return mustParseProfiles(t, `
aspect "audit" {
  when = "after"
  on   = ["create_*"]
`+body+`
}
`)
}

func TestAnActionWritesThroughAConnector(t *testing.T) {
	cfg := aspectFrom(t, `
  action {
    connector = "audit_db"
    operation = "INSERT"
    target    = "audit_log"

    transform {
      flow_name = "_flow"
      at        = "now()"
    }
  }`)

	if len(cfg.Aspects) != 1 {
		t.Fatalf("%d aspects parsed", len(cfg.Aspects))
	}
	action := cfg.Aspects[0].Action
	if action == nil {
		t.Fatal("the action block was not read")
	}
	if action.Connector != "audit_db" || action.Operation != "INSERT" || action.Target != "audit_log" {
		t.Errorf("action = %+v", action)
	}
	if len(action.Transform) != 2 {
		t.Errorf("transform = %v, want both mappings", action.Transform)
	}
}

func TestAnActionCanHandOffToAFlow(t *testing.T) {
	// The other shape: rather than writing somewhere itself, the aspect runs a
	// flow that already knows how.
	cfg := aspectFrom(t, `
  action {
    flow = "record_audit"
  }`)

	action := cfg.Aspects[0].Action
	if action.Flow != "record_audit" {
		t.Errorf("flow = %q", action.Flow)
	}
	if action.Connector != "" {
		t.Errorf("connector = %q, want none", action.Connector)
	}
}

func TestAnActionCannotBeBothAtOnce(t *testing.T) {
	// A connector and a flow are two different things to do, and doing one of
	// them silently would be a coin toss.
	_, err := parsed(t, `
aspect "audit" {
  when = "after"
  on   = ["create_*"]

  action {
    connector = "audit_db"
    flow      = "record_audit"
  }
}
`)
	if err == nil {
		t.Fatal("an action naming both a connector and a flow was accepted")
	}
	if !strings.Contains(err.Error(), "flow") {
		t.Errorf("error = %q, want it to say what the conflict is", err)
	}
}

func TestAnActionThatNamesNothingIsRefused(t *testing.T) {
	// It would match flows, run, and do nothing — with the configuration
	// saying an audit trail was being written.
	_, err := parsed(t, `
aspect "audit" {
  when = "after"
  on   = ["create_*"]

  action {
    operation = "INSERT"
    target    = "audit_log"
  }
}
`)
	if err == nil {
		t.Error("an action with nothing to act on was accepted")
	}
}

func TestATransformInsideAnActionShapesWhatIsWritten(t *testing.T) {
	// The audit row is built here, and these are the variables an aspect has:
	// the flow it wrapped, the result it produced, and the moment.
	cfg := aspectFrom(t, `
  action {
    connector = "audit_db"
    operation = "INSERT"
    target    = "audit_log"

    transform {
      flow      = "_flow"
      operation = "_operation"
      target    = "_target"
      at        = "_timestamp"
      outcome   = "has(result.id) ? 'written' : 'nothing'"
    }
  }`)

	transform := cfg.Aspects[0].Action.Transform
	for _, field := range []string{"flow", "operation", "target", "at", "outcome"} {
		if transform[field] == "" {
			t.Errorf("the %q mapping was dropped: %v", field, transform)
		}
	}
}

package parser

import (
	"strings"
	"testing"
)

// Category C: mapping-style blocks. `accept` fits the registry pattern; named
// `response` is a *ResponseConfig while a flow's inline response stays a bare
// map (the reference is carried by the ResponseUse marker), so its resolver is
// bespoke. error_response is intentionally NOT independently reusable — it is
// covered by reusing the whole error_handling block (which holds a single
// error_response).

// --- accept ---

func TestNamedAcceptResolvesAndOverrides(t *testing.T) {
	hcl := `
accept "is_mine" {
  when      = "input.body.type == 'order'"
  on_reject = "reject"
}

flow "resolves" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  accept {
    use = "accept.is_mine"
  }
}

flow "overrides" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  accept {
    use       = "accept.is_mine"
    on_reject = "requeue"
  }
}
`
	cfg := mustParseDir(t, hcl)

	r := findFlow(t, cfg, "resolves").Accept
	if r.When != "input.body.type == 'order'" || r.OnReject != "reject" {
		t.Errorf("resolved accept should inherit base: %+v", r)
	}
	if r.Use != "is_mine" {
		t.Errorf("Use should be preserved for tracing: got %q", r.Use)
	}

	ov := findFlow(t, cfg, "overrides").Accept
	if ov.OnReject != "requeue" {
		t.Errorf("on_reject override: want requeue, got %q", ov.OnReject)
	}
	// when not overridden → inherits base.
	if ov.When != "input.body.type == 'order'" {
		t.Errorf("when should inherit base: got %q", ov.When)
	}
}

func TestNamedAcceptMissingWhenFails(t *testing.T) {
	hcl := `
accept "broken" {
  on_reject = "ack"
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "when") {
		t.Errorf("expected when error for named accept; got: %v", err)
	}
}

func TestFlowAcceptUnknownNameFails(t *testing.T) {
	hcl := `
accept "is_mine" {
  when = "true"
}

flow "typo" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  accept {
    use = "accept.is_minee"
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "is_minee") || !strings.Contains(err.Error(), "is_mine") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}

func TestInlineAcceptWithoutUseStillSelfContained(t *testing.T) {
	hcl := `
flow "still_inline" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  accept {
    when = "input.ok"
  }
}
`
	cfg := mustParseDir(t, hcl)
	a := cfg.Flows[0].Accept
	if a.When != "input.ok" {
		t.Errorf("inline accept lost when: %+v", a)
	}
	if a.OnReject != "ack" {
		t.Errorf("inline accept default on_reject: want ack, got %q", a.OnReject)
	}
	if a.Use != "" {
		t.Errorf("inline-only accept should have empty Use, got %q", a.Use)
	}
}

// --- response ---

func TestNamedResponseResolvesAndOverrides(t *testing.T) {
	hcl := `
response "envelope" {
  id     = "output.id"
  status = "'ok'"
  ts     = "output.created_at"
}

flow "resolves" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  response {
    use = "response.envelope"
  }
}

flow "overrides" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  response {
    use    = "response.envelope"
    status = "'created'"
    extra  = "output.extra"
  }
}
`
	cfg := mustParseDir(t, hcl)

	r := findFlow(t, cfg, "resolves").Response
	if len(r) != 3 || r["id"] != "output.id" || r["status"] != "'ok'" || r["ts"] != "output.created_at" {
		t.Errorf("resolved response should equal named base mappings: %+v", r)
	}
	if findFlow(t, cfg, "resolves").ResponseUse != "envelope" {
		t.Errorf("ResponseUse marker should be preserved for tracing")
	}

	ov := findFlow(t, cfg, "overrides").Response
	// status overridden, id/ts inherited, extra added (4 keys, no "use" leak).
	if len(ov) != 4 {
		t.Errorf("merged response should have 4 keys, got %d: %+v", len(ov), ov)
	}
	if ov["status"] != "'created'" {
		t.Errorf("status override: want 'created', got %q", ov["status"])
	}
	if ov["id"] != "output.id" || ov["ts"] != "output.created_at" {
		t.Errorf("untouched response keys should inherit base: %+v", ov)
	}
	if ov["extra"] != "output.extra" {
		t.Errorf("inline-only response key should be added: %+v", ov)
	}
	if _, leaked := ov["use"]; leaked {
		t.Errorf("the `use` key must not leak into the response map: %+v", ov)
	}
}

func TestNamedResponseEmptyFails(t *testing.T) {
	hcl := `
response "empty" {
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "at least one field") {
		t.Errorf("expected empty-response error; got: %v", err)
	}
}

func TestFlowResponseUnknownNameFails(t *testing.T) {
	hcl := `
response "envelope" {
  id = "output.id"
}

flow "typo" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  response {
    use = "response.envelopee"
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "envelopee") || !strings.Contains(err.Error(), "envelope") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}

func TestInlineResponseWithoutUseStillWorks(t *testing.T) {
	hcl := `
flow "still_inline" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  response {
    result = "step.results"
  }
}
`
	cfg := mustParseDir(t, hcl)
	f := cfg.Flows[0]
	if f.Response["result"] != "step.results" {
		t.Errorf("inline response lost mapping: %+v", f.Response)
	}
	if f.ResponseUse != "" {
		t.Errorf("inline-only response should have empty ResponseUse, got %q", f.ResponseUse)
	}
}

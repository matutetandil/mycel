package parser

import "testing"

// Quoting a CEL expression is the documented form. An unquoted one still has to
// survive the trip through the parser intact, because the failure mode is not a
// parse error — it is an expression that lost part of itself and goes on
// producing a plausible wrong answer.

func TestUnquotedExpressionKeepsFunctionCall(t *testing.T) {
	cfg := parseOne(t, `
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  transform {
    name  = upper(input.name)
    plain = input.name
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`)
	mappings := cfg.Flows[0].Transform.Mappings
	if got := mappings["name"]; got != "upper(input.name)" {
		t.Errorf("function call dropped: name = %q, want %q", got, "upper(input.name)")
	}
	if got := mappings["plain"]; got != "input.name" {
		t.Errorf("bare traversal mangled: plain = %q, want %q", got, "input.name")
	}
}

func TestUnquotedConditionKeepsComparison(t *testing.T) {
	cfg := parseOne(t, `
flow "big_orders" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "db"
    target    = "orders"
    when      = output.total > 1000
  }
}
`)
	if got := cfg.Flows[0].To.When; got != "output.total > 1000" {
		t.Errorf("comparison dropped: when = %q, want %q", got, "output.total > 1000")
	}
}

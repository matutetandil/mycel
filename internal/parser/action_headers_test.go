package parser

import "testing"

// `headers` on a saga action and on a state machine transition action, kept
// where the executors read them.
func TestHeadersOnSagaAndStateMachineActionsAreKept(t *testing.T) {
	cfg := mustParse(t, `
saga "order" {
  step "charge" {
    action {
      connector = "stripe"
      operation = "POST /charges"
      body      = { amount = "input.amount" }
      headers   = { Store = "input.store" }
    }
    compensate {
      connector = "stripe"
      operation = "POST /refunds"
      headers   = { Store = "input.store" }
    }
  }
  on_complete {
    connector = "erp"
    operation = "POST /done"
    headers   = { Store = "input.store" }
  }
}

state_machine "order_status" {
  initial = "new"
  state "new" {
    on "ship" {
      transition_to = "shipped"
      action {
        connector = "erp"
        operation = "POST /ship"
        headers   = { "X-Tenant" = "input.tenant" }
      }
    }
  }
  state "shipped" {}
}
`)
	s := cfg.Sagas[0]
	for name, a := range map[string]interface{}{
		"action": s.Steps[0].Action.Headers, "compensate": s.Steps[0].Compensate.Headers, "on_complete": s.OnComplete.Headers,
	} {
		hdrs, _ := a.(map[string]interface{})
		if hdrs["Store"] != "input.store" {
			t.Errorf("saga %s: headers = %#v", name, hdrs)
		}
	}
	action := cfg.StateMachines[0].States["new"].Transitions["ship"].Action
	if action == nil || action.Headers["X-Tenant"] != "input.tenant" {
		t.Errorf("state machine action headers = %#v", action)
	}
}

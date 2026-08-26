package examples

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A flow whose destination is a transaction still gets to say what it answers.
//
// The transactional write returned straight out of the dispatch, past
// everything that happens to an answer on its way: the `response` block was
// ignored, so a flow that shaped its reply got the write's row counts instead,
// and `validate { output }` was never checked. Silently — the block was
// parsed, the editor offered completions inside it, and nothing ran it.
func TestATransactionalFlowShapesItsAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "reusable-blocks")
	port := svc.ports[8080]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	status, answer := svc.run(t, fmt.Sprintf(
		`curl -X POST %s/stock -H 'Content-Type: application/json' `+
			`-d '{"tenant":"acme","stage":"ingest","id":"SKU-1","name":"Widget","price":19.9,"job_id":10}'`, base))
	if status != 200 {
		t.Fatalf("the write answered %d: %s", status, answer)
	}

	var shaped map[string]interface{}
	if err := json.Unmarshal([]byte(answer), &shaped); err != nil {
		t.Fatalf("the answer was not JSON: %v: %s", err, answer)
	}

	// What the flow's response block asks for, rather than the write's own
	// {"affected":2,"captured":{}}.
	if shaped["status"] != "ok" {
		t.Errorf("the response block did not run: %s", answer)
	}
	if shaped["id"] != "SKU-1" {
		t.Errorf("id = %v, want the one the request carried: %s", shaped["id"], answer)
	}
	if written, ok := shaped["written"].(float64); !ok || written != 2 {
		t.Errorf("written = %v, want the two statements the transaction ran: %s", shaped["written"], answer)
	}
}

// The gates in that same flow answer without spilling their internals.
func TestADroppedRequestSaysWhichGateDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "reusable-blocks")
	port := svc.ports[8080]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	status, answer := svc.run(t, fmt.Sprintf(
		`curl -X POST %s/stock -H 'Content-Type: application/json' `+
			`-d '{"tenant":"somebody-else","stage":"ingest","id":"SKU-9","name":"No","price":1,"job_id":1}'`, base))
	if status != 200 {
		t.Fatalf("the refused request answered %d: %s", status, answer)
	}

	var shaped map[string]interface{}
	if err := json.Unmarshal([]byte(answer), &shaped); err != nil {
		t.Fatalf("the answer was not JSON: %v: %s", err, answer)
	}
	if shaped["status"] != "dropped" || shaped["reason"] != "accept" {
		t.Errorf("a request the accept gate refused answered %s", answer)
	}
	if strings.Contains(answer, "input.tenant") {
		t.Errorf("the answer hands the caller the rule it failed: %s", answer)
	}
}

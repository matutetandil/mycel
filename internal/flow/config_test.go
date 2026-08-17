package flow

import (
	"context"
	"errors"
	"testing"
)

// Reading a flow's configuration back out.
//
// Everything a from, to or step block carries is kept in one map of
// connector parameters, and read through these accessors — so what a
// destination writes to, what a step queries, and whether a message is
// filtered all come through here. A getter that reads the wrong key or the
// wrong type does not fail: it returns nothing, and the flow runs without
// the setting somebody wrote.

var errAlreadyExists = errors.New("duplicate key value violates unique constraint")

func TestWhatADestinationWasToldToDo(t *testing.T) {
	to := &ToConfig{ConnectorParams: map[string]interface{}{
		"target":       "orders",
		"operation":    "INSERT",
		"format":       "json",
		"filter":       "output.total > 0",
		"query":        "INSERT INTO orders (id) VALUES (:id)",
		"query_filter": map[string]interface{}{"status": "open"},
		"update":       map[string]interface{}{"status": "paid"},
		"params":       map[string]interface{}{"id": "output.id"},
	}}

	if to.GetTarget() != "orders" || to.GetOperation() != "INSERT" || to.GetFormat() != "json" {
		t.Errorf("target/operation/format = %s/%s/%s", to.GetTarget(), to.GetOperation(), to.GetFormat())
	}
	if to.GetFilter() != "output.total > 0" {
		t.Errorf("filter = %q", to.GetFilter())
	}
	if to.GetQuery() == "" {
		t.Error("the query went missing")
	}
	if to.GetQueryFilter()["status"] != "open" {
		t.Errorf("query filter = %v", to.GetQueryFilter())
	}
	if to.GetUpdate()["status"] != "paid" {
		t.Errorf("update = %v", to.GetUpdate())
	}
	if to.GetParams()["id"] != "output.id" {
		t.Errorf("params = %v", to.GetParams())
	}
}

func TestADestinationThatWasToldNothing(t *testing.T) {
	// A `to` block with a transaction in it has no target, no query and no
	// operation — the parser refuses that combination — so empty is the
	// normal answer here, not a failure.
	empty := &ToConfig{}

	if empty.GetTarget() != "" || empty.GetOperation() != "" || empty.GetQuery() != "" {
		t.Errorf("a destination with no parameters answered with something: %+v", empty)
	}
	if empty.GetParams() != nil || empty.GetQueryFilter() != nil || empty.GetUpdate() != nil {
		t.Error("a destination with no parameters answered with a map")
	}

	// And a value of the wrong kind reads as absent rather than being
	// returned as something a caller then treats as text.
	wrongShape := &ToConfig{ConnectorParams: map[string]interface{}{
		"target": 42,
		"params": "not a record",
	}}
	if wrongShape.GetTarget() != "" || wrongShape.GetParams() != nil {
		t.Errorf("a parameter of the wrong kind was passed on: %s / %v",
			wrongShape.GetTarget(), wrongShape.GetParams())
	}
}

func TestWhatASourceIsListeningFor(t *testing.T) {
	from := &FromConfig{ConnectorParams: map[string]interface{}{
		"operation": "GET /orders",
		"format":    "json",
	}}

	if from.GetOperation() != "GET /orders" || from.GetFormat() != "json" {
		t.Errorf("operation/format = %s/%s", from.GetOperation(), from.GetFormat())
	}
}

func TestWhichFilterAFlowIsUsing(t *testing.T) {
	// There are two ways to write one, and the block form has to win: a flow
	// carrying both and honouring the older attribute would apply a condition
	// its author replaced.
	both := &FromConfig{
		Filter:       "input.origin != 'internal'",
		FilterConfig: &FilterConfig{Condition: "input.total > 100"},
	}
	if both.FilterCondition() != "input.total > 100" {
		t.Errorf("condition = %q, want the block's", both.FilterCondition())
	}

	older := &FromConfig{Filter: "input.origin != 'internal'"}
	if older.FilterCondition() != "input.origin != 'internal'" {
		t.Errorf("condition = %q", older.FilterCondition())
	}

	if (&FromConfig{}).FilterCondition() != "" {
		t.Error("a source with no filter reported one")
	}
}

func TestWhatAStepWasToldToFetch(t *testing.T) {
	step := &StepConfig{ConnectorParams: map[string]interface{}{
		"operation": "SELECT",
		"target":    "customers",
		"format":    "json",
		"query":     "SELECT * FROM customers WHERE id = :id",
		"body":      map[string]interface{}{"id": "input.customer_id"},
		"params":    map[string]interface{}{"id": "input.customer_id"},
	}}

	if step.GetOperation() != "SELECT" || step.GetTarget() != "customers" || step.GetFormat() != "json" {
		t.Errorf("step = %+v", step.ConnectorParams)
	}
	if step.GetQuery() == "" {
		t.Error("the step's query went missing")
	}
	if step.GetBody()["id"] != "input.customer_id" {
		t.Errorf("body = %v", step.GetBody())
	}
	if step.GetParams()["id"] != "input.customer_id" {
		t.Errorf("params = %v", step.GetParams())
	}

	bare := &StepConfig{}
	if bare.GetQuery() != "" || bare.GetBody() != nil {
		t.Error("a step with no parameters answered with something")
	}
}

func TestAnEmptyRequestAndResponse(t *testing.T) {
	// Every map has to be there to be written into; a nil one panics at the
	// first header set on it.
	in := NewInput()
	if in.Data == nil || in.Headers == nil || in.Params == nil || in.Query == nil {
		t.Fatalf("input = %+v", in)
	}
	in.Headers["X-Request-Id"] = "request-1"
	in.Data["order_id"] = "order-1"

	out := NewOutput(map[string]interface{}{"id": "order-1"})
	// A successful answer is a 200 unless something says otherwise, and a
	// zero status code would be sent as one by net/http and read as a
	// protocol error by anything else.
	if out.StatusCode != 200 || out.Headers == nil || out.Error != "" {
		t.Errorf("output = %+v", out)
	}

	failed := NewErrorOutput(422, "email is not an email")
	if failed.StatusCode != 422 || failed.Error != "email is not an email" {
		t.Errorf("error output = %+v", failed)
	}
	if failed.Headers == nil {
		t.Error("a failed answer cannot carry headers")
	}
}

func TestAnAspectForADroppedMessageFiresOnce(t *testing.T) {
	// A message dropped by a filter, an accept gate, or a coordinate timeout
	// can have an aspect attached to say so. It has to fire once: when a
	// message fans out to several flows the one that decides the outcome
	// fires it, and firing again would send a second notification for the
	// same dropped message.
	fired := 0
	result := &FilteredResultWithPolicy{
		Policy:        "ack",
		PendingOnDrop: func(ctx context.Context) { fired++ },
	}

	if !FireDropAspect(context.Background(), result) {
		t.Fatal("the aspect did not fire")
	}
	if FireDropAspect(context.Background(), result) {
		t.Error("the aspect fired a second time for one dropped message")
	}
	if fired != 1 {
		t.Errorf("the aspect ran %d times", fired)
	}

	// Anything else passes straight through: consumers hand over whatever the
	// handler returned without looking at it first.
	if FireDropAspect(context.Background(), nil) {
		t.Error("nothing fired an aspect")
	}
	if FireDropAspect(context.Background(), map[string]interface{}{"id": "order-1"}) {
		t.Error("an ordinary result fired a drop aspect")
	}
	if FireDropAspect(context.Background(), &FilteredResultWithPolicy{Policy: "ack"}) {
		t.Error("a result with no aspect attached fired one")
	}
}

func TestADropThatTravelsAsAnError(t *testing.T) {
	// A dropped message is wrapped as an error where it has to cross a layer
	// that only looks at errors — the aspect executor — while still carrying
	// the disposition the queue consumer needs to decide between ack, requeue
	// and reject.
	dropped := &FilteredDropError{Result: &FilteredResultWithPolicy{Policy: "requeue"}}

	if dropped.Error() == "" {
		t.Error("the error says nothing")
	}
	if got := dropped.Error(); got != "filtered drop (policy=requeue)" {
		t.Errorf("error = %q, want it to name the disposition", got)
	}

	// It must not panic on the empty forms, which is what a layer that
	// wraps errors will eventually hand it.
	if (&FilteredDropError{}).Error() == "" {
		t.Error("an empty drop error says nothing")
	}
	var missing *FilteredDropError
	if missing.Error() == "" {
		t.Error("a nil drop error says nothing")
	}
}

func TestTheTransformOutputTravelsWithTheFlow(t *testing.T) {
	// What `output.*` means in a coordinate signal: what the transform
	// produced, not what the destination answered. The slot is how the
	// wrapper that evaluates the signal gets hold of it.
	slot := &OutputSlot{}
	ctx := WithOutputCapture(context.Background(), slot)

	if got := TransformOutputFromContext(ctx).Get(); got != nil {
		t.Errorf("something was captured before the transform ran: %v", got)
	}

	TransformOutputFromContext(ctx).Set(map[string]interface{}{"total": 1500})

	captured := slot.Get()
	if captured == nil || captured["total"] != 1500 {
		t.Errorf("captured = %v, want what the transform produced", captured)
	}

	// A flow nobody is capturing for must not panic: this runs on every
	// message, and most of them have no wrapper. Both the missing slot and
	// the missing context have to be safe.
	if got := TransformOutputFromContext(context.Background()); got != nil {
		t.Errorf("a flow with no capture answered with %v", got)
	}
	TransformOutputFromContext(context.Background()).Set(map[string]interface{}{"total": 1})
	if got := TransformOutputFromContext(context.Background()).Get(); got != nil {
		t.Errorf("a flow with no capture answered with %v", got)
	}
}

func TestEveryLoopVariableInATransactionIsNamed(t *testing.T) {
	// The runtime declares these in the CEL scope before the transaction
	// runs. One missed and the statement referring to it fails to compile —
	// mid-transaction, after earlier statements have already run.
	tx := &TransactionConfig{Statements: []TxStatement{
		{Exec: &TxExec{Query: "INSERT INTO orders (id) VALUES (:id)"}},
		{Each: &TxEach{Var: "item", In: "output.items", Body: []TxStatement{
			{Exec: &TxExec{Query: "INSERT INTO order_items (sku) VALUES (:sku)"}},
			// Loops nest: a line's serial numbers inside an order's lines.
			{Each: &TxEach{Var: "serial", In: "item.serials", Body: []TxStatement{
				{Exec: &TxExec{Query: "INSERT INTO serials (n) VALUES (:n)"}},
			}}},
		}}},
		{Each: &TxEach{Var: "payment", In: "output.payments"}},
	}}

	names := tx.EachVarNames()

	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	for _, want := range []string{"item", "serial", "payment"} {
		if !found[want] {
			t.Errorf("%s is a loop variable and was not declared: %v", want, names)
		}
	}
	if len(names) != 3 {
		t.Errorf("names = %v, want one per loop", names)
	}

	// A transaction with no loops, and no transaction at all.
	plain := &TransactionConfig{Statements: []TxStatement{{Exec: &TxExec{Query: "DELETE FROM orders"}}}}
	if got := plain.EachVarNames(); len(got) != 0 {
		t.Errorf("a transaction with no loops declared %v", got)
	}
	var missing *TransactionConfig
	if got := missing.EachVarNames(); got != nil {
		t.Errorf("a flow with no transaction declared %v", got)
	}
}

func TestAFailureThatCarriesTheAnswerToSendBack(t *testing.T) {
	// An error_response turns a failure into a reply somebody chose: the
	// status, the body and the headers a caller receives instead of a bare
	// 500 with a stack of wrapped error text.
	underlying := errAlreadyExists
	failure := NewFlowError(underlying, 409,
		map[string]interface{}{"error": "that order already exists"},
		map[string]string{"X-Order-Id": "order-1"})

	if failure.Status != 409 {
		t.Errorf("status = %d", failure.Status)
	}
	if failure.Body["error"] == nil || failure.Headers["X-Order-Id"] != "order-1" {
		t.Errorf("failure = %+v", failure)
	}
	if failure.Error() == "" {
		t.Error("the failure says nothing")
	}
	// The original has to remain reachable: retry and error_handling decide
	// what to do by looking at what actually went wrong.
	if failure.Unwrap() == nil || failure.Unwrap().Error() != underlying.Error() {
		t.Errorf("the original failure was lost: %v", failure.Unwrap())
	}

	// A failure that names no status is a server error, not a zero — which
	// net/http sends as 200.
	if got := NewFlowError(underlying, 0, nil, nil); got.Status != 500 {
		t.Errorf("status = %d, want 500", got.Status)
	}
}

func TestWhatAnEnrichmentWasToldToFetch(t *testing.T) {
	enrich := &EnrichConfig{ConnectorParams: map[string]interface{}{"operation": "SELECT"}}
	if enrich.GetOperation() != "SELECT" {
		t.Errorf("operation = %q", enrich.GetOperation())
	}
	if (&EnrichConfig{}).GetOperation() != "" {
		t.Error("an enrichment with no operation reported one")
	}
}

func TestValuesCarriedAlongsideOneExecution(t *testing.T) {
	ctx := NewContext("place_order")

	if ctx.FlowName != "place_order" || ctx.Values == nil {
		t.Fatalf("context = %+v", ctx)
	}
	if _, ok := ctx.Get("anything"); ok {
		t.Error("a value nobody set came back")
	}

	ctx.Set("tenant", "acme")
	value, ok := ctx.Get("tenant")
	if !ok || value != "acme" {
		t.Errorf("value = %v, %v", value, ok)
	}
}

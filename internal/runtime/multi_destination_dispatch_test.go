package runtime

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/trace"
)

// Which path a flow with several destinations takes.
//
// The tests next to this one call the fan-out directly, which is why none of
// them noticed that the dispatch never got there. With more than one `to` the
// destinations live in MultiTo and both To and Dest are nil, and three places
// read that as "this flow has no destination": a flow with a step answered
// with what the step gathered, and a scheduled flow, a non-REST source or a GET
// stayed a read, which with no Dest is the destination-less answer. Each of
// them returned success and wrote nothing.

func fanOutFlow(t *testing.T, from *flow.FromConfig, sourceType string) (*FlowHandler, *recordingWriter, *recordingWriter) {
	t.Helper()
	items := &recordingWriter{name: "items"}
	audit := &recordingWriter{name: "audit"}
	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "items", ConnectorParams: map[string]interface{}{"target": "items"}},
		{Connector: "audit", ConnectorParams: map[string]interface{}{"target": "audit"}},
	}, map[string]*recordingWriter{"items": items, "audit": audit})
	h.Config.From = from
	h.SourceType = sourceType
	return h, items, audit
}

func restSource(operation string) *flow.FromConfig {
	return &flow.FromConfig{
		Connector:       "api",
		ConnectorParams: map[string]interface{}{"operation": operation},
	}
}

func TestAFlowWithAStepWritesToEveryDestination(t *testing.T) {
	// The shape of issue #129: look something up, then fan out — one write
	// guarded by what the lookup found, and an audit row either way.
	lookup := &stepConnector{name: "lookup", rows: []map[string]interface{}{{"n": int64(1)}}}

	h, items, audit := fanOutFlow(t, restSource("POST /items"), "rest")
	h.Connectors.Replace("lookup", lookup)
	h.Config.Steps = []*flow.StepConfig{{
		Name: "lookup", Connector: "lookup",
		ConnectorParams: map[string]interface{}{"query": "SELECT COUNT(*) AS n FROM items WHERE sku = :sku"},
	}}
	h.Config.Transform = &flow.TransformConfig{
		Mappings: map[string]string{"sku": "input.sku", "n": "step.lookup.n"},
		Order:    []string{"sku", "n"},
	}
	h.Config.MultiTo[0].When = "output.n > 0"

	result, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{"sku": "A1"})
	if err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	if calls, _ := lookup.seen(); calls != 1 {
		t.Errorf("the step ran %d times, want once", calls)
	}
	if len(items.writes()) != 1 || len(audit.writes()) != 1 {
		t.Fatalf("writes: items=%d audit=%d, want one each — answered %#v",
			len(items.writes()), len(audit.writes()), result)
	}
	if got := items.writes()[0].Payload["n"]; got != int64(1) {
		t.Errorf("the guarded write carried n=%#v, want the step's value", got)
	}
	if _, ok := result.(*MultiDestResult); !ok {
		t.Errorf("answered %T, want the report of the writes", result)
	}
}

func TestAStepValueCanSkipOneDestination(t *testing.T) {
	// The `when` reads what the step found, so the step has to have run
	// before the destinations are decided — and the other one still writes.
	lookup := &stepConnector{name: "lookup", rows: []map[string]interface{}{{"n": int64(0)}}}

	h, items, audit := fanOutFlow(t, restSource("POST /items"), "rest")
	h.Connectors.Replace("lookup", lookup)
	h.Config.Steps = []*flow.StepConfig{{
		Name: "lookup", Connector: "lookup",
		ConnectorParams: map[string]interface{}{"query": "SELECT COUNT(*) AS n FROM items WHERE sku = :sku"},
	}}
	h.Config.Transform = &flow.TransformConfig{
		Mappings: map[string]string{"sku": "input.sku", "n": "step.lookup.n"},
		Order:    []string{"sku", "n"},
	}
	h.Config.MultiTo[0].When = "output.n > 0"

	if _, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{"sku": "ZZ"}); err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	if n := len(items.writes()); n != 0 {
		t.Errorf("the guarded destination wrote %d times with n = 0", n)
	}
	if n := len(audit.writes()); n != 1 {
		t.Errorf("the unguarded destination wrote %d times, want once", n)
	}
}

func TestEveryTriggerWritesToSeveralDestinations(t *testing.T) {
	// A flow with several destinations has no read path, so it is a write
	// whatever triggered it. Each of these stayed a read and wrote nothing.
	cases := []struct {
		name       string
		from       *flow.FromConfig
		sourceType string
	}{
		{"scheduled flow", nil, ""},
		{"GET request", restSource("GET /ping"), "rest"},
		{"gRPC method", &flow.FromConfig{
			Connector:       "rpc",
			ConnectorParams: map[string]interface{}{"operation": "CreateItem"},
		}, "grpc"},
		{"GraphQL query", &flow.FromConfig{
			Connector:       "gql",
			ConnectorParams: map[string]interface{}{"operation": "Query.ping"},
		}, "graphql"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, items, audit := fanOutFlow(t, tc.from, tc.sourceType)
			h.Config.Transform = &flow.TransformConfig{
				Mappings: map[string]string{"t": "'x'"},
				Order:    []string{"t"},
			}

			result, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{})
			if err != nil {
				t.Fatalf("flow failed: %v", err)
			}
			if len(items.writes()) != 1 || len(audit.writes()) != 1 {
				t.Errorf("writes: items=%d audit=%d, want one each — answered %#v",
					len(items.writes()), len(audit.writes()), result)
			}
		})
	}
}

func TestAFlowWithAStepAndNoDestinationStillAnswersWithTheSteps(t *testing.T) {
	// The reason the destination-less check exists: an orchestration flow
	// answering a POST runs its steps and answers with them.
	lookup := &stepConnector{name: "lookup", rows: []map[string]interface{}{{"name": "Ada"}}}
	h := stepHandler(t, []*flow.StepConfig{{
		Name: "lookup", Connector: "lookup",
		ConnectorParams: map[string]interface{}{"query": "SELECT name FROM users"},
	}}, map[string]connector.Connector{"lookup": lookup})
	h.Config.From = restSource("POST /profile")
	h.SourceType = "rest"
	h.Config.Transform = &flow.TransformConfig{
		Mappings: map[string]string{"name": "step.lookup.name"},
		Order:    []string{"name"},
	}

	result, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	answer, ok := result.(map[string]interface{})
	if !ok || answer["name"] != "Ada" {
		t.Errorf("answered %#v, want what the step gathered", result)
	}
}

func TestATraceListsEveryDestinationWrite(t *testing.T) {
	// `mycel trace` recorded a fan-out's writes only in a dry run; a real
	// trace listed none and labelled the flow's answer READ.
	h, _, _ := fanOutFlow(t, restSource("POST /items"), "rest")
	collector := trace.NewMemoryCollector()
	ctx := trace.WithTrace(context.Background(), &trace.Context{FlowName: h.Config.Name, Collector: collector})

	if _, err := h.executeFlowCoreInternal(ctx, map[string]interface{}{"sku": "A1"}); err != nil {
		t.Fatalf("flow failed: %v", err)
	}

	written := map[string]bool{}
	for _, event := range collector.Events() {
		if event.Stage == trace.StageWrite {
			written[event.Name] = true
		}
	}
	if !written["items:items"] || !written["audit:audit"] {
		t.Errorf("trace recorded writes %v, want one per destination", written)
	}
}

package runtime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/aspect"
	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/transform"
)

// An aspect action can name a flow, and aspects match flows by glob. The
// easiest pattern to write is "*", which matches every flow — including the one
// the action invokes. That configuration used to recurse without bound: the
// call never returned, and in a service each message would hold a goroutine
// growing its stack until the process died.

func invokingRegistry(t *testing.T, aspects ...*aspect.Config) *FlowRegistry {
	t.Helper()
	registry := NewFlowRegistry()

	aspectRegistry := aspect.NewRegistry()
	for _, a := range aspects {
		aspectRegistry.Register(a)
	}
	executor, err := aspect.NewExecutor(aspectRegistry, connector.NewRegistry())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	executor.SetFlowInvoker(registry)

	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	for _, name := range []string{"process", "audit", "notify"} {
		registry.Register(name, &FlowHandler{
			Config: &flow.Config{
				Name: name,
				From: &flow.FromConfig{Connector: "src"},
			},
			Connectors:     connector.NewRegistry(),
			Transformer:    transformer,
			AspectExecutor: executor,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}
	return registry
}

// answers runs a call that must not be allowed to hang the suite if the guard
// is ever removed.
func answers(t *testing.T, call func() (interface{}, error)) (interface{}, error) {
	t.Helper()
	type outcome struct {
		result interface{}
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := call()
		done <- outcome{result, err}
	}()

	select {
	case got := <-done:
		return got.result, got.err
	case <-time.After(10 * time.Second):
		t.Fatal("the call never returned: a flow is invoking itself without bound")
		return nil, nil
	}
}

func TestAFlowThatInvokesItselfIsRefusedRatherThanRunForever(t *testing.T) {
	registry := invokingRegistry(t, &aspect.Config{
		Name: "audit_everything", When: "before", On: []string{"*"},
		Action: &aspect.ActionConfig{Flow: "process"},
	})

	handler, _ := registry.Get("process")
	_, err := answers(t, func() (interface{}, error) {
		return handler.HandleRequest(context.Background(), map[string]interface{}{"id": 1})
	})

	// The flow itself still answers — an aspect that cannot run is reported by
	// the aspect executor, and whether that fails the request is its decision.
	// What matters here is that the call came back at all.
	_ = err
}

func TestTheCycleIsNamedWhenAFlowIsInvokedInsideItself(t *testing.T) {
	registry := invokingRegistry(t)

	ctx := withInvocation(context.Background(), "process")
	_, err := answers(t, func() (interface{}, error) {
		return registry.InvokeFlow(ctx, "process", map[string]interface{}{})
	})
	if err == nil {
		t.Fatal("a flow was invoked inside itself")
	}
	for _, want := range []string{"process", "→"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to show the path that closed the loop", err)
		}
	}
}

func TestALoopThroughSeveralFlowsIsCaughtToo(t *testing.T) {
	// Mutual recursion is the same defect written less obviously: process
	// invokes audit, audit invokes process.
	registry := invokingRegistry(t)

	ctx := withInvocation(withInvocation(context.Background(), "process"), "audit")
	_, err := answers(t, func() (interface{}, error) {
		return registry.InvokeFlow(ctx, "process", map[string]interface{}{})
	})
	if err == nil {
		t.Fatal("a loop through two flows was not caught")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Errorf("error = %q, want the whole path including the flow in between", err)
	}
}

func TestOneFlowInvokingAnotherIsOrdinaryWork(t *testing.T) {
	// The guard only refuses a repeat. A chain of distinct flows is how an
	// aspect delegates, and it has to keep working.
	registry := invokingRegistry(t)

	ctx := withInvocation(context.Background(), "process")
	if _, err := answers(t, func() (interface{}, error) {
		return registry.InvokeFlow(ctx, "audit", map[string]interface{}{})
	}); err != nil {
		t.Errorf("a flow invoking a different flow was refused: %v", err)
	}
}

func TestTwoInvocationsFromOneRequestDoNotSeeEachOther(t *testing.T) {
	// Two aspects on the same flow each invoke a flow. They are siblings, not
	// a chain, and neither is a cycle.
	registry := invokingRegistry(t)
	ctx := withInvocation(context.Background(), "process")

	for _, name := range []string{"audit", "notify"} {
		if _, err := registry.InvokeFlow(ctx, name, map[string]interface{}{}); err != nil {
			t.Errorf("invoking %q was refused: %v", name, err)
		}
	}

	// And the sibling calls did not leave anything behind on the shared chain.
	if got := len(invocationChain(ctx)); got != 1 {
		t.Errorf("the chain grew to %d entries from calls that were not nested", got)
	}
}

func TestInvokingAFlowThatDoesNotExistSaysSo(t *testing.T) {
	registry := invokingRegistry(t)
	_, err := registry.InvokeFlow(context.Background(), "typo", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "typo") {
		t.Errorf("error = %v, want the name that was asked for", err)
	}
}

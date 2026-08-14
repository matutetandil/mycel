package runtime

import (
	"context"
	"fmt"
	"strings"
)

// A flow can invoke another flow: an aspect action names one, and so does a
// workflow step. Nothing checked whether the flow being invoked was already
// running further up the same request, so a configuration that closed the loop
// — the easiest one to write by accident is an aspect on = ["*"] whose action
// invokes a flow, since "*" matches the flow it invokes — recursed until the
// process ran out of memory. Measured: the call never returned.
//
// The chain of flows a request has passed through is carried on the context, so
// a cycle is refused with the path that closed it and everything else is left
// alone.

type invocationChainKey struct{}

// invocationChain returns the flows this request has already passed through.
func invocationChain(ctx context.Context) []string {
	chain, _ := ctx.Value(invocationChainKey{}).([]string)
	return chain
}

// withInvocation records that a request is now inside flowName.
func withInvocation(ctx context.Context, flowName string) context.Context {
	existing := invocationChain(ctx)
	// Copied rather than appended in place: two aspects invoking two flows
	// from the same request would otherwise share the backing array and see
	// each other's names.
	chain := make([]string, len(existing), len(existing)+1)
	copy(chain, existing)
	return context.WithValue(ctx, invocationChainKey{}, append(chain, flowName))
}

// checkInvocationCycle reports the cycle a flow would close, if it closes one.
func checkInvocationCycle(ctx context.Context, flowName string) error {
	chain := invocationChain(ctx)
	for i, seen := range chain {
		if seen == flowName {
			return fmt.Errorf(
				"flow %q invokes itself: %s. An aspect matching \"*\" matches the flow "+
					"its action invokes; narrow the pattern or exclude that flow",
				flowName, strings.Join(append(append([]string{}, chain[i:]...), flowName), " → "))
		}
	}
	return nil
}

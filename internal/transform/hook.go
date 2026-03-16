package transform

import "context"

// TransformHook allows external systems (debuggers) to observe and control
// individual CEL rule evaluations within a transform stage.
//
// When no hook is present in the context, the cost is a single context.Value
// lookup (~10ns) per rule — negligible compared to CEL evaluation (~µs).
type TransformHook interface {
	// BeforeRule is called before each CEL rule evaluation.
	// index is the 0-based rule index within the transform.
	// activation contains the current CEL variables (input, output, enriched, step).
	// Returns false to abort the transform.
	BeforeRule(ctx context.Context, index int, rule Rule, activation map[string]interface{}) bool

	// AfterRule is called after each CEL rule evaluation.
	// result is the evaluated value (nil on error). err is non-nil on evaluation failure.
	AfterRule(ctx context.Context, index int, rule Rule, result interface{}, err error)
}

// transformHookKey is the context key for TransformHook.
type transformHookKey struct{}

// WithTransformHook returns a context with the given TransformHook attached.
func WithTransformHook(ctx context.Context, hook TransformHook) context.Context {
	return context.WithValue(ctx, transformHookKey{}, hook)
}

// HookFromContext retrieves the TransformHook from a context.
// Returns nil if no hook is present.
func HookFromContext(ctx context.Context) TransformHook {
	hook, _ := ctx.Value(transformHookKey{}).(TransformHook)
	return hook
}

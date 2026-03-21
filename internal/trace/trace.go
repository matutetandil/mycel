// Package trace provides flow execution tracing for debugging Mycel configurations.
// It captures data at each pipeline stage (sanitize, validate, transform, read/write)
// and supports multiple output modes: CLI trace, verbose logging, dry-run, and breakpoints.
//
// The trace context lives in Go's context.Context. When absent, the overhead is
// a single nil-check per stage — essentially zero cost for production.
package trace

import (
	"context"
	"errors"
	"time"
)

// ErrBreakpointAbort is returned when the user aborts execution from a breakpoint.
var ErrBreakpointAbort = errors.New("execution aborted at breakpoint")

var errBreakpointAbort = ErrBreakpointAbort

// Stage represents a pipeline stage in flow execution.
type Stage string

const (
	StageInput       Stage = "input"
	StageSanitize    Stage = "sanitize"
	StageFilter      Stage = "filter"
	StageAccept      Stage = "accept"
	StageDedupe      Stage = "dedupe"
	StageValidateIn  Stage = "validate_input"
	StageEnrich      Stage = "enrich"
	StageTransform   Stage = "transform"
	StageStep        Stage = "step"
	StageValidateOut Stage = "validate_output"
	StageRead        Stage = "read"
	StageWrite       Stage = "write"
	StageCacheHit    Stage = "cache_hit"
	StageCacheMiss   Stage = "cache_miss"
)

// Event represents a single trace event captured during flow execution.
type Event struct {
	// Stage is the pipeline stage where this event occurred.
	Stage Stage

	// Name is a sub-name for the event (step name, enrichment name, etc.).
	Name string

	// Input is a snapshot of the data going INTO this stage.
	Input interface{}

	// Output is a snapshot of the data coming OUT of this stage.
	Output interface{}

	// Duration is how long this stage took.
	Duration time.Duration

	// Error is any error that occurred during this stage.
	Error error

	// Skipped indicates the stage was skipped (filter=false, optimization, etc.).
	Skipped bool

	// DryRun indicates this stage was simulated (no actual side effects).
	DryRun bool

	// Detail contains additional stage-specific information.
	Detail string
}

// BreakpointController controls interactive debugging at pipeline stages.
// Implementations: Breakpoint (CLI stdin), DAPBreakpoint (IDE via DAP).
type BreakpointController interface {
	// ShouldBreak returns true if execution should pause at the given stage.
	ShouldBreak(stage Stage) bool

	// Pause blocks execution at a breakpoint. Returns false to abort.
	Pause(stage Stage, name string, data interface{}) bool
}

// Context holds trace state for a single request execution.
type Context struct {
	// FlowName is the name of the flow being traced.
	FlowName string

	// Collector receives trace events as they are recorded.
	Collector Collector

	// DryRun prevents write operations from executing.
	DryRun bool

	// Breakpoint enables interactive debugging at pipeline stages.
	// When set, execution pauses at breakpoints and waits for user input or IDE commands.
	Breakpoint BreakpointController
}

// Record is a convenience method to record an event on this trace context.
func (tc *Context) Record(event Event) {
	if tc != nil && tc.Collector != nil {
		tc.Collector.Record(event)
	}
}

// context key for trace context
type contextKey struct{}

// WithTrace returns a new context with the trace context attached.
func WithTrace(ctx context.Context, tc *Context) context.Context {
	return context.WithValue(ctx, contextKey{}, tc)
}

// FromContext retrieves the trace context from a Go context.
// Returns nil if no trace context is present.
func FromContext(ctx context.Context) *Context {
	tc, _ := ctx.Value(contextKey{}).(*Context)
	return tc
}

// IsTracing returns true if the context has an active trace.
func IsTracing(ctx context.Context) bool {
	return FromContext(ctx) != nil
}

// RecordStage is a helper that records a trace event with timing.
// If no trace context is present, the function is called directly with zero overhead.
// If a breakpoint is set for this stage, execution pauses before the function runs.
func RecordStage(ctx context.Context, stage Stage, name string, input interface{}, fn func() (interface{}, error)) (interface{}, error) {
	tc := FromContext(ctx)
	if tc == nil {
		return fn()
	}

	// Check breakpoint before executing the stage
	if tc.Breakpoint != nil && tc.Breakpoint.ShouldBreak(stage) {
		if !tc.Breakpoint.Pause(stage, name, input) {
			return nil, errBreakpointAbort
		}
	}

	start := time.Now()
	output, err := fn()
	tc.Record(Event{
		Stage:    stage,
		Name:     name,
		Input:    snapshot(input),
		Output:   snapshot(output),
		Duration: time.Since(start),
		Error:    err,
	})
	return output, err
}

// RecordSimple records a trace event without wrapping a function call.
// If a breakpoint is set for this stage, execution pauses.
func RecordSimple(ctx context.Context, stage Stage, name string, data interface{}, detail string) {
	tc := FromContext(ctx)
	if tc == nil {
		return
	}

	// Check breakpoint
	if tc.Breakpoint != nil && tc.Breakpoint.ShouldBreak(stage) {
		tc.Breakpoint.Pause(stage, name, data)
	}

	tc.Record(Event{
		Stage:  stage,
		Name:   name,
		Output: snapshot(data),
		Detail: detail,
	})
}

// RecordSkipped records a skipped stage.
func RecordSkipped(ctx context.Context, stage Stage, name string, detail string) {
	tc := FromContext(ctx)
	if tc == nil {
		return
	}
	tc.Record(Event{
		Stage:   stage,
		Name:    name,
		Skipped: true,
		Detail:  detail,
	})
}

// Snapshot creates a shallow copy of data for trace recording.
// Exported for use by instrumentation points outside this package.
func Snapshot(data interface{}) interface{} {
	return snapshot(data)
}

// snapshot creates a shallow copy of data for trace recording.
// Returns nil for nil input. For maps, creates a shallow copy to avoid
// mutations affecting the trace record.
func snapshot(data interface{}) interface{} {
	if data == nil {
		return nil
	}
	switch v := data.(type) {
	case map[string]interface{}:
		copy := make(map[string]interface{}, len(v))
		for k, val := range v {
			copy[k] = val
		}
		return copy
	case []map[string]interface{}:
		copy := make([]map[string]interface{}, len(v))
		for i, row := range v {
			rowCopy := make(map[string]interface{}, len(row))
			for k, val := range row {
				rowCopy[k] = val
			}
			copy[i] = rowCopy
		}
		return copy
	default:
		return data
	}
}

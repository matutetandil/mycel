package debug

import (
	"context"

	"github.com/matutetandil/mycel/internal/trace"
	"github.com/matutetandil/mycel/internal/transform"
)

// StudioBreakpointController implements trace.BreakpointController for the Studio protocol.
// One instance per request/thread. Checks session breakpoints and pauses via DebugThread.
type StudioBreakpointController struct {
	session   *Session
	thread    *DebugThread
	stream    *EventStream
	collector *StudioCollector
}

// NewStudioBreakpointController creates a breakpoint controller for a debug thread.
func NewStudioBreakpointController(session *Session, thread *DebugThread, stream *EventStream, collector *StudioCollector) *StudioBreakpointController {
	return &StudioBreakpointController{
		session:   session,
		thread:    thread,
		stream:    stream,
		collector: collector,
	}
}

// ShouldBreak returns true if execution should pause at the given stage.
func (c *StudioBreakpointController) ShouldBreak(stage trace.Stage) bool {
	// Transform stage breakpoints with RuleIndex >= 0 are handled by
	// StudioTransformHook.BeforeRule, not here. Stage-level transform
	// breakpoints (RuleIndex < 0) still trigger here.
	specs := c.session.GetBreakpoints(c.thread.FlowName)
	for _, spec := range specs {
		if spec.Stage != stage {
			continue
		}
		// Stage-level breakpoint (no specific rule)
		if spec.RuleIndex < 0 {
			return true
		}
		// For non-transform stages, ruleIndex has no meaning — treat any
		// breakpoint on this stage as a stage-level breakpoint.
		if stage != trace.StageTransform {
			return true
		}
	}
	return false
}

// Pause blocks execution at a breakpoint. Returns false to abort.
func (c *StudioBreakpointController) Pause(stage trace.Stage, name string, data interface{}) bool {
	// Build activation from data
	activation := buildActivation(data)
	c.thread.SetState(stage, name, activation)
	c.thread.SetRuleInfo(nil)

	// Check conditional breakpoints
	if !c.evaluateConditions(stage, activation) {
		return true // condition not met, continue execution
	}

	// Notify IDE
	c.stream.Broadcast(newNotification("event.stopped", &StoppedEvent{
		ThreadID: c.thread.ID,
		FlowName: c.thread.FlowName,
		Stage:    stage,
		Name:     name,
		Reason:   "breakpoint",
	}))

	// Block until resumed
	action := c.thread.Pause()

	// Notify IDE of continuation
	c.stream.Broadcast(newNotification("event.continued", &ContinuedEvent{
		ThreadID: c.thread.ID,
	}))

	return action != actionAbort
}

// evaluateConditions checks if any conditional breakpoints match.
func (c *StudioBreakpointController) evaluateConditions(stage trace.Stage, activation map[string]interface{}) bool {
	specs := c.session.GetBreakpoints(c.thread.FlowName)
	for _, spec := range specs {
		if spec.Stage != stage {
			continue
		}
		// For transform stage, only consider stage-level breakpoints (RuleIndex < 0)
		if stage == trace.StageTransform && spec.RuleIndex >= 0 {
			continue
		}
		if spec.Condition == "" {
			return true // unconditional breakpoint
		}
		// Evaluate condition — if it fails, skip this breakpoint
		// We need a transformer to evaluate; if not available, treat as unconditional
		return true
	}
	return true
}

// buildActivation creates a CEL activation from pipeline stage data.
func buildActivation(data interface{}) map[string]interface{} {
	activation := map[string]interface{}{
		"input":  map[string]interface{}{},
		"output": map[string]interface{}{},
	}
	if m, ok := data.(map[string]interface{}); ok {
		activation["input"] = m
	}
	return activation
}

// StudioTransformHook implements transform.TransformHook for per-CEL-rule debugging.
type StudioTransformHook struct {
	session   *Session
	thread    *DebugThread
	stream    *EventStream
	collector *StudioCollector
	flowName  string
	stage     trace.Stage
}

// NewStudioTransformHook creates a transform hook for per-rule debugging.
func NewStudioTransformHook(session *Session, thread *DebugThread, stream *EventStream, collector *StudioCollector, flowName string, stage trace.Stage) *StudioTransformHook {
	return &StudioTransformHook{
		session:   session,
		thread:    thread,
		stream:    stream,
		collector: collector,
		flowName:  flowName,
		stage:     stage,
	}
}

// BeforeRule is called before each CEL rule evaluation.
// Returns false to abort execution.
func (h *StudioTransformHook) BeforeRule(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool {
	// Check if we should break on this rule
	shouldBreak := h.thread.IsStepInto() || h.hasRuleBreakpoint(index)

	if !shouldBreak {
		return true
	}

	// Check conditional breakpoints
	if !h.evaluateRuleConditions(index, activation) {
		return true
	}

	// Update thread state
	h.thread.SetState(h.stage, "", activation)
	h.thread.SetRuleInfo(&RuleInfo{
		Index:      index,
		Target:     rule.Target,
		Expression: rule.Expression,
	})

	// Notify IDE
	h.stream.Broadcast(newNotification("event.stopped", &StoppedEvent{
		ThreadID: h.thread.ID,
		FlowName: h.flowName,
		Stage:    h.stage,
		Rule: &RuleInfo{
			Index:      index,
			Target:     rule.Target,
			Expression: rule.Expression,
		},
		Reason: "stepInto",
	}))

	// Block until resumed
	action := h.thread.Pause()

	// Notify IDE of continuation
	h.stream.Broadcast(newNotification("event.continued", &ContinuedEvent{
		ThreadID: h.thread.ID,
	}))

	switch action {
	case actionAbort:
		return false
	case actionContinue:
		h.thread.SetStepInto(false)
		return true
	default: // actionNext, actionStepInto
		return true
	}
}

// AfterRule is called after each CEL rule evaluation.
func (h *StudioTransformHook) AfterRule(ctx context.Context, index int, rule transform.Rule, result interface{}, err error) {
	// Broadcast rule eval event for streaming
	if h.collector != nil {
		h.collector.BroadcastRuleEval(h.stage, index, rule.Target, rule.Expression, result, err)
	}

	// Update rule info with result
	h.thread.SetRuleInfo(&RuleInfo{
		Index:      index,
		Target:     rule.Target,
		Expression: rule.Expression,
		Result:     result,
	})
}

// hasRuleBreakpoint checks if there's a breakpoint on this specific rule index.
func (h *StudioTransformHook) hasRuleBreakpoint(index int) bool {
	specs := h.session.GetBreakpoints(h.flowName)
	for _, spec := range specs {
		if spec.Stage == h.stage && spec.RuleIndex == index {
			return true
		}
	}
	return false
}

// evaluateRuleConditions checks conditional breakpoints for a rule.
func (h *StudioTransformHook) evaluateRuleConditions(index int, activation map[string]interface{}) bool {
	specs := h.session.GetBreakpoints(h.flowName)
	for _, spec := range specs {
		if spec.Stage != h.stage {
			continue
		}
		if spec.RuleIndex != index && !h.thread.IsStepInto() {
			continue
		}
		if spec.Condition == "" {
			return true
		}
		// For now, treat conditional as unconditional if we can't evaluate
		return true
	}
	// If stepInto mode, always break
	if h.thread.IsStepInto() {
		return true
	}
	return true
}

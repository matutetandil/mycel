package debug

import (
	"context"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/trace"
	"github.com/matutetandil/mycel/v2/internal/transform"
)

// Stopping inside a transform, on one rule.
//
// A transform is a list of CEL expressions, and the debugger can stop at any
// one of them — with a condition, so somebody chasing a single bad record does
// not stop on every message. That condition was ignored: every path through
// the check returned true, so a breakpoint asking to stop on rule 12 only when
// `input.email != ""` — the example in the protocol documentation — stopped on
// rule 12 every time. In a transform of forty rules that is the difference
// between one pause and forty.

func hookFor(t *testing.T, specs []BreakpointSpec) (*StudioTransformHook, *DebugThread) {
	t.Helper()

	session := NewSession("session-1", "studio")
	session.SetBreakpoints("normalise_order", specs)
	thread := NewDebugThread("thread-1", "normalise_order")

	return NewStudioTransformHook(session, thread, NewEventStream(), nil, "normalise_order", trace.StageTransform), thread
}

// runRule calls BeforeRule and reports whether it paused. Anything that pauses
// is resumed from another goroutine, so a test that would otherwise hang fails
// with a message instead.
func runRule(t *testing.T, hook *StudioTransformHook, thread *DebugThread, index int, activation map[string]interface{}) bool {
	t.Helper()
	return runRuleResuming(t, hook, thread, index, activation, actionContinue)
}

// runRuleResuming is the same with a say in how the pause ends, which matters
// while stepping: continuing is what leaves step mode.
func runRuleResuming(t *testing.T, hook *StudioTransformHook, thread *DebugThread, index int, activation map[string]interface{}, resumeWith resumeAction) bool {
	t.Helper()

	paused := make(chan bool, 1)
	go func() {
		select {
		case <-time.After(200 * time.Millisecond):
			paused <- false
		default:
		}
		thread.WaitForPause()
		paused <- true
		thread.Resume(resumeWith)
	}()

	done := make(chan struct{})
	go func() {
		hook.BeforeRule(context.Background(), index,
			transform.Rule{Target: "email", Expression: "lower(input.email)"}, activation)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the rule never finished: something paused and nothing resumed it")
	}

	select {
	case did := <-paused:
		return did
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

func TestABreakpointOnOneRuleWithACondition(t *testing.T) {
	hook, thread := hookFor(t, []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: 2, Condition: `input.email != ""`},
	})

	// The record somebody is looking for.
	if !runRule(t, hook, thread, 2, map[string]interface{}{
		"input": map[string]interface{}{"email": "someone@example.test"},
	}) {
		t.Error("the breakpoint did not stop on the record its condition describes")
	}

	// And every other record, which is the whole point of the condition.
	if runRule(t, hook, thread, 2, map[string]interface{}{
		"input": map[string]interface{}{"email": ""},
	}) {
		t.Error("the breakpoint stopped on a record its condition excludes")
	}
}

func TestABreakpointWithNoConditionStopsEveryTime(t *testing.T) {
	hook, thread := hookFor(t, []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: 0},
	})

	if !runRule(t, hook, thread, 0, map[string]interface{}{"input": map[string]interface{}{}}) {
		t.Error("an unconditional breakpoint did not stop")
	}
}

func TestOtherRulesRunStraightThrough(t *testing.T) {
	// The rule either side of the one somebody asked about.
	hook, thread := hookFor(t, []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: 5},
	})

	for _, index := range []int{4, 6, 40} {
		if runRule(t, hook, thread, index, map[string]interface{}{"input": map[string]interface{}{}}) {
			t.Errorf("rule %d stopped, and the breakpoint is on rule 5", index)
		}
	}
}

func TestAConditionThatCannotBeEvaluatedStopsAnyway(t *testing.T) {
	// A condition naming something that is not there is a question somebody
	// asked and did not get an answer to. Stopping is the recoverable half of
	// being wrong; carrying on silently is not.
	hook, thread := hookFor(t, []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: 1, Condition: `input.nothing.at.all == "x"`},
	})

	if !runRule(t, hook, thread, 1, map[string]interface{}{"input": map[string]interface{}{}}) {
		t.Error("a breakpoint whose condition could not be evaluated was skipped in silence")
	}
}

func TestSteppingThroughEveryRule(t *testing.T) {
	// Step into is the other way to stop inside a transform: no breakpoint,
	// every rule, one at a time.
	hook, thread := hookFor(t, nil)
	thread.SetStepInto(true)

	for _, index := range []int{0, 1, 2} {
		if !runRuleResuming(t, hook, thread, index,
			map[string]interface{}{"input": map[string]interface{}{}}, actionStepInto) {
			t.Errorf("stepping did not stop at rule %d", index)
		}
	}

	// Continuing is what leaves step mode: otherwise the next forty rules
	// stop too, and the only way out is to disconnect.
	runRuleResuming(t, hook, thread, 3, map[string]interface{}{"input": map[string]interface{}{}}, actionContinue)
	if thread.IsStepInto() {
		t.Error("continuing left the debugger stepping through every rule")
	}
	if runRule(t, hook, thread, 4, map[string]interface{}{"input": map[string]interface{}{}}) {
		t.Error("a rule stopped after the debugger was told to continue")
	}
}

func TestWhatARulePausedOnIsVisible(t *testing.T) {
	// What the debugger shows while stopped: which rule, what it is computing,
	// and the values it can see.
	hook, thread := hookFor(t, []BreakpointSpec{{Stage: trace.StageTransform, RuleIndex: 3}})

	activation := map[string]interface{}{
		"input":  map[string]interface{}{"email": "SOMEONE@EXAMPLE.TEST"},
		"output": map[string]interface{}{"id": "order-1"},
	}
	runRule(t, hook, thread, 3, activation)

	variables := thread.GetVariables()
	if variables.Rule == nil || variables.Rule.Index != 3 {
		t.Fatalf("rule = %+v, want the one it stopped on", variables.Rule)
	}
	if variables.Rule.Target != "email" || variables.Rule.Expression == "" {
		t.Errorf("the rule does not say what it computes: %+v", variables.Rule)
	}
	input, ok := variables.Input.(map[string]interface{})
	if !ok || input["email"] != "SOMEONE@EXAMPLE.TEST" {
		t.Errorf("input = %v, want what the rule can see", variables.Input)
	}
	if variables.Output == nil {
		t.Error("what has been built so far is not visible")
	}

	// And after the rule ran, its result is on the same record.
	hook.AfterRule(context.Background(), 3,
		transform.Rule{Target: "email", Expression: "lower(input.email)"}, "someone@example.test", nil)

	if after := thread.GetVariables().Rule; after == nil || after.Result != "someone@example.test" {
		t.Errorf("the rule's result was not recorded: %+v", after)
	}
}

func TestStageLevelBreakpointsStillWorkOnTransforms(t *testing.T) {
	// A breakpoint on the transform stage as a whole is written with a rule
	// index below zero, and is handled by the controller rather than the hook.
	// The two must not both stop on the same message.
	session := NewSession("session-1", "studio")
	session.SetBreakpoints("normalise_order", []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: -1},
	})
	thread := NewDebugThread("thread-1", "normalise_order")
	controller := NewStudioBreakpointController(session, thread, NewEventStream(), nil)

	if !controller.ShouldBreak(trace.StageTransform) {
		t.Error("a breakpoint on the transform stage did not stop")
	}
	if controller.ShouldBreak(trace.StageWrite) {
		t.Error("it stopped on a stage nobody asked about")
	}

	// A per-rule breakpoint is the hook's business: the controller must leave
	// it alone, or the message stops twice for one breakpoint.
	perRule := NewSession("session-2", "studio")
	perRule.SetBreakpoints("normalise_order", []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: 2},
	})
	if NewStudioBreakpointController(perRule, thread, NewEventStream(), nil).ShouldBreak(trace.StageTransform) {
		t.Error("the controller stopped on a breakpoint that belongs to a rule")
	}

	// On any other stage a rule index means nothing, so the breakpoint counts.
	elsewhere := NewSession("session-3", "studio")
	elsewhere.SetBreakpoints("normalise_order", []BreakpointSpec{
		{Stage: trace.StageWrite, RuleIndex: 2},
	})
	if !NewStudioBreakpointController(elsewhere, thread, NewEventStream(), nil).ShouldBreak(trace.StageWrite) {
		t.Error("a breakpoint on a stage that has no rules was ignored")
	}
}

func TestAStageBreakpointWithACondition(t *testing.T) {
	session := NewSession("session-1", "studio")
	session.SetBreakpoints("normalise_order", []BreakpointSpec{
		{Stage: trace.StageWrite, RuleIndex: -1, Condition: `input.total > 1000`},
	})
	thread := NewDebugThread("thread-1", "normalise_order")
	controller := NewStudioBreakpointController(session, thread, NewEventStream(), nil)

	big := map[string]interface{}{"input": map[string]interface{}{"total": 5000}}
	small := map[string]interface{}{"input": map[string]interface{}{"total": 10}}

	if !controller.evaluateConditions(trace.StageWrite, big) {
		t.Error("the breakpoint did not stop on the record its condition describes")
	}
	if controller.evaluateConditions(trace.StageWrite, small) {
		t.Error("the breakpoint stopped on a record its condition excludes")
	}
	// A condition that cannot be evaluated stops rather than being skipped.
	broken := map[string]interface{}{"input": map[string]interface{}{}}
	if !controller.evaluateConditions(trace.StageWrite, broken) {
		t.Error("a condition that could not be evaluated was skipped in silence")
	}
}

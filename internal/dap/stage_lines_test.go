package dap

import (
	"testing"

	"github.com/matutetandil/mycel/v2/internal/trace"
)

// A debug client sets breakpoints by line, so a stage with no line is a stage a
// client cannot stop at.
//
// StageLines was a hand-written list that had fallen behind the stages: accept
// was missing, so a breakpoint on it resolved to line 0 — which is no line at
// all, and quietly meant no breakpoint.

func TestEveryStageAClientCanStopAtHasALine(t *testing.T) {
	// The stages a flow can pause at. cache_hit and cache_miss are outcomes
	// reported after the fact rather than points execution reaches, so they are
	// not breakpoint targets.
	stoppable := []trace.Stage{
		trace.StageInput, trace.StageSanitize, trace.StageFilter, trace.StageAccept,
		trace.StageDedupe, trace.StageValidateIn, trace.StageEnrich,
		trace.StageTransform, trace.StageStep, trace.StageValidateOut,
		trace.StageRead, trace.StageWrite, trace.StageResponse,
	}

	for _, stage := range stoppable {
		line, ok := StageLines[string(stage)]
		if !ok {
			t.Errorf("stage %q has no line, so a debug client cannot set a breakpoint on it", stage)
			continue
		}
		if line == 0 {
			t.Errorf("stage %q is mapped to line 0, which is not a line", stage)
		}
	}
}

func TestNoTwoStagesShareALine(t *testing.T) {
	seen := make(map[int]string, len(StageLines))
	for stage, line := range StageLines {
		if other, clash := seen[line]; clash {
			t.Errorf("%q and %q are both line %d, so a breakpoint on one stops at the other", stage, other, line)
		}
		seen[line] = stage
	}
	if len(LineToStage) != len(StageLines) {
		t.Errorf("the reverse mapping has %d entries for %d stages, so a line is lost", len(LineToStage), len(StageLines))
	}
}

func TestALineMapsBackToItsStage(t *testing.T) {
	for stage, line := range StageLines {
		if got := LineToStage[line]; got != stage {
			t.Errorf("line %d maps back to %q, not %q", line, got, stage)
		}
	}
}

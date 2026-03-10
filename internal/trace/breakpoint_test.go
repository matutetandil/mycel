package trace

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestBreakpointShouldBreakAll(t *testing.T) {
	bp := NewBreakpoint(strings.NewReader("n\n"), &bytes.Buffer{})

	if !bp.ShouldBreak(StageInput) {
		t.Error("expected break at input")
	}
	if !bp.ShouldBreak(StageTransform) {
		t.Error("expected break at transform")
	}
	if !bp.ShouldBreak(StageWrite) {
		t.Error("expected break at write")
	}
}

func TestBreakpointShouldBreakStages(t *testing.T) {
	bp := NewBreakpointForStages(
		[]Stage{StageTransform, StageWrite},
		strings.NewReader("n\n"),
		&bytes.Buffer{},
	)

	if bp.ShouldBreak(StageInput) {
		t.Error("should not break at input")
	}
	if bp.ShouldBreak(StageSanitize) {
		t.Error("should not break at sanitize")
	}
	if !bp.ShouldBreak(StageTransform) {
		t.Error("expected break at transform")
	}
	if !bp.ShouldBreak(StageWrite) {
		t.Error("expected break at write")
	}
}

func TestBreakpointNil(t *testing.T) {
	var bp *Breakpoint
	if bp.ShouldBreak(StageInput) {
		t.Error("nil breakpoint should not break")
	}
	// Pause on nil should return true (continue)
	if !bp.Pause(StageInput, "", nil) {
		t.Error("nil breakpoint Pause should return true")
	}
}

func TestBreakpointPauseNext(t *testing.T) {
	var out bytes.Buffer
	bp := NewBreakpoint(strings.NewReader("n\n"), &out)

	result := bp.Pause(StageTransform, "", map[string]interface{}{"x": 1})
	if !result {
		t.Error("'next' command should continue execution")
	}
	if !strings.Contains(out.String(), "BREAKPOINT") {
		t.Error("expected breakpoint header in output")
	}
}

func TestBreakpointPauseContinue(t *testing.T) {
	var out bytes.Buffer
	bp := NewBreakpoint(strings.NewReader("c\n"), &out)

	result := bp.Pause(StageTransform, "", nil)
	if !result {
		t.Error("'continue' command should continue execution")
	}
	if !bp.skip {
		t.Error("expected skip=true after continue")
	}

	// After continue, ShouldBreak returns false for BreakAll
	if bp.ShouldBreak(StageWrite) {
		t.Error("should skip after continue in BreakAll mode")
	}
}

func TestBreakpointPauseAbort(t *testing.T) {
	var out bytes.Buffer
	bp := NewBreakpoint(strings.NewReader("q\n"), &out)

	result := bp.Pause(StageTransform, "", nil)
	if result {
		t.Error("'quit' command should abort execution")
	}
}

func TestBreakpointPauseHelp(t *testing.T) {
	var out bytes.Buffer
	// Send help then next
	bp := NewBreakpoint(strings.NewReader("h\nn\n"), &out)

	result := bp.Pause(StageTransform, "my_step", nil)
	if !result {
		t.Error("should continue after help+next")
	}
	if !strings.Contains(out.String(), "Commands:") {
		t.Error("expected help text in output")
	}
}

func TestBreakpointPausePrint(t *testing.T) {
	var out bytes.Buffer
	bp := NewBreakpoint(strings.NewReader("p\nn\n"), &out)

	data := map[string]interface{}{"email": "test@example.com"}
	result := bp.Pause(StageInput, "", data)
	if !result {
		t.Error("should continue after print+next")
	}
	// Data should appear at least twice (initial display + print command)
	count := strings.Count(out.String(), "test@example.com")
	if count < 2 {
		t.Errorf("expected data printed at least twice, got %d", count)
	}
}

func TestBreakpointContinueStopsAtExplicitBreakpoint(t *testing.T) {
	var out bytes.Buffer
	// Stages: break at transform and write only
	bp := NewBreakpointForStages(
		[]Stage{StageTransform, StageWrite},
		strings.NewReader("c\nn\n"), // continue at transform, next at write
		&out,
	)

	// First breakpoint at transform
	if !bp.ShouldBreak(StageTransform) {
		t.Error("should break at transform")
	}
	bp.Pause(StageTransform, "", nil) // sends "c" (continue)

	// Skip non-breakpoint stages
	if bp.ShouldBreak(StageRead) {
		t.Error("should skip read after continue")
	}

	// Should stop at next explicit breakpoint
	if !bp.ShouldBreak(StageWrite) {
		t.Error("should break at write (explicit breakpoint)")
	}
}

func TestParseBreakStages(t *testing.T) {
	stages := ParseBreakStages("input,transform,write")
	if len(stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(stages))
	}
	if stages[0] != StageInput {
		t.Errorf("stages[0] = %q, want %q", stages[0], StageInput)
	}
	if stages[1] != StageTransform {
		t.Errorf("stages[1] = %q, want %q", stages[1], StageTransform)
	}
	if stages[2] != StageWrite {
		t.Errorf("stages[2] = %q, want %q", stages[2], StageWrite)
	}
}

func TestParseBreakStagesEmpty(t *testing.T) {
	stages := ParseBreakStages("")
	if stages != nil {
		t.Errorf("expected nil for empty string, got %v", stages)
	}
}

func TestRecordStageWithBreakpointAbort(t *testing.T) {
	var out bytes.Buffer
	bp := NewBreakpoint(strings.NewReader("q\n"), &out)
	tc := &Context{
		FlowName:   "test",
		Collector:  NewMemoryCollector(),
		Breakpoint: bp,
	}
	ctx := WithTrace(context.Background(), tc)

	_, err := RecordStage(ctx, StageTransform, "", nil, func() (interface{}, error) {
		t.Error("function should not execute after abort")
		return nil, nil
	})

	if err == nil {
		t.Error("expected error after breakpoint abort")
	}
	if err != ErrBreakpointAbort {
		t.Errorf("expected ErrBreakpointAbort, got %v", err)
	}
}

func TestRecordStageWithBreakpointContinue(t *testing.T) {
	var out bytes.Buffer
	bp := NewBreakpoint(strings.NewReader("n\n"), &out)
	tc := &Context{
		FlowName:   "test",
		Collector:  NewMemoryCollector(),
		Breakpoint: bp,
	}
	ctx := WithTrace(context.Background(), tc)

	executed := false
	result, err := RecordStage(ctx, StageTransform, "", nil, func() (interface{}, error) {
		executed = true
		return "done", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("function should have executed after 'next'")
	}
	if result != "done" {
		t.Errorf("expected 'done', got %v", result)
	}
}

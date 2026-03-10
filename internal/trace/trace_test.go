package trace

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestContextPlumbing(t *testing.T) {
	ctx := context.Background()

	// No trace context initially
	if IsTracing(ctx) {
		t.Error("expected no trace in background context")
	}
	if FromContext(ctx) != nil {
		t.Error("expected nil trace context")
	}

	// Attach trace context
	tc := &Context{
		FlowName:  "test_flow",
		Collector: NewMemoryCollector(),
	}
	ctx = WithTrace(ctx, tc)

	if !IsTracing(ctx) {
		t.Error("expected tracing to be active")
	}
	got := FromContext(ctx)
	if got != tc {
		t.Error("expected same trace context back")
	}
	if got.FlowName != "test_flow" {
		t.Errorf("expected flow name 'test_flow', got %q", got.FlowName)
	}
}

func TestMemoryCollector(t *testing.T) {
	c := NewMemoryCollector()

	c.Record(Event{Stage: StageInput, Output: map[string]interface{}{"key": "val"}})
	c.Record(Event{Stage: StageSanitize, Duration: 100 * time.Microsecond})
	c.Record(Event{Stage: StageWrite, Error: fmt.Errorf("write failed")})

	events := c.Events()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Stage != StageInput {
		t.Errorf("event 0 stage = %q, want %q", events[0].Stage, StageInput)
	}
	if events[2].Error == nil {
		t.Error("event 2 should have an error")
	}
}

func TestRecordStage(t *testing.T) {
	tc := &Context{
		FlowName:  "test",
		Collector: NewMemoryCollector(),
	}
	ctx := WithTrace(context.Background(), tc)

	result, err := RecordStage(ctx, StageTransform, "", map[string]interface{}{"a": 1}, func() (interface{}, error) {
		return map[string]interface{}{"b": 2}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}

	events := tc.Collector.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Stage != StageTransform {
		t.Errorf("stage = %q, want %q", events[0].Stage, StageTransform)
	}
	if events[0].Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestRecordStageNoTrace(t *testing.T) {
	// Without trace context, function executes normally
	ctx := context.Background()
	result, err := RecordStage(ctx, StageTransform, "", nil, func() (interface{}, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
}

func TestRecordSkipped(t *testing.T) {
	tc := &Context{
		FlowName:  "test",
		Collector: NewMemoryCollector(),
	}
	ctx := WithTrace(context.Background(), tc)

	RecordSkipped(ctx, StageValidateIn, "", "no schema configured")

	events := tc.Collector.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].Skipped {
		t.Error("expected skipped=true")
	}
	if events[0].Detail != "no schema configured" {
		t.Errorf("detail = %q, want %q", events[0].Detail, "no schema configured")
	}
}

func TestSnapshot(t *testing.T) {
	original := map[string]interface{}{"key": "val"}
	snap := snapshot(original)

	// Modify original
	original["key"] = "changed"

	// Snapshot should be unchanged
	snapMap := snap.(map[string]interface{})
	if snapMap["key"] != "val" {
		t.Error("snapshot was mutated by original change")
	}
}

func TestSnapshotNil(t *testing.T) {
	if snapshot(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestRenderer(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf)

	events := []Event{
		{Stage: StageInput, Output: map[string]interface{}{"email": "test@example.com"}},
		{Stage: StageSanitize, Duration: 50 * time.Microsecond, Output: map[string]interface{}{"email": "test@example.com"}},
		{Stage: StageValidateIn, Skipped: true, Detail: "no schema"},
		{Stage: StageTransform, Duration: 100 * time.Microsecond, Output: map[string]interface{}{"id": "abc", "email": "test@example.com"}},
		{Stage: StageWrite, Name: "users", Duration: 5 * time.Millisecond, Output: map[string]interface{}{"affected": 1}},
	}

	r.Render("create_user", events, 6*time.Millisecond)

	output := buf.String()
	if output == "" {
		t.Fatal("expected non-empty output")
	}

	// Check key elements are present
	checks := []string{"Flow: create_user", "INPUT", "SANITIZE", "VALIDATE INPUT", "skipped", "TRANSFORM", "WRITE → users", "completed successfully"}
	for _, check := range checks {
		if !bytes.Contains(buf.Bytes(), []byte(check)) {
			t.Errorf("output missing %q", check)
		}
	}
}

func TestRendererWithError(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf)

	events := []Event{
		{Stage: StageInput, Output: map[string]interface{}{"x": 1}},
		{Stage: StageWrite, Error: fmt.Errorf("connection refused"), Duration: 2 * time.Millisecond},
	}

	r.Render("failing_flow", events, 3*time.Millisecond)

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("connection refused")) {
		t.Error("expected error message in output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("completed with errors")) {
		t.Error("expected error summary")
	}
	_ = output
}

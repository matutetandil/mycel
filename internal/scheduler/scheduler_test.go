package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseWhen_Always(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"always"},
		{"  always  "},
	}

	for _, tc := range tests {
		triggerType, err := ParseWhen(tc.input)
		if err != nil {
			t.Errorf("ParseWhen(%q) unexpected error: %v", tc.input, err)
		}
		if triggerType != TriggerAlways {
			t.Errorf("ParseWhen(%q) = %v, want TriggerAlways", tc.input, triggerType)
		}
	}
}

func TestParseWhen_Interval(t *testing.T) {
	tests := []struct {
		input    string
		wantErr  bool
	}{
		{"@every 5m", false},
		{"@every 1h", false},
		{"@every 30s", false},
		{"@every 100ms", false},
		{"@every invalid", true},
		{"@every ", true},
	}

	for _, tc := range tests {
		triggerType, err := ParseWhen(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseWhen(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseWhen(%q) unexpected error: %v", tc.input, err)
			}
			if triggerType != TriggerInterval {
				t.Errorf("ParseWhen(%q) = %v, want TriggerInterval", tc.input, triggerType)
			}
		}
	}
}

func TestParseWhen_Cron(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		// Standard cron expressions
		{"0 0 * * *", false},
		{"*/5 * * * *", false},
		{"0 3 * * *", false},
		{"0 */2 * * *", false},

		// Shortcuts
		{"@yearly", false},
		{"@annually", false},
		{"@monthly", false},
		{"@weekly", false},
		{"@daily", false},
		{"@midnight", false},
		{"@hourly", false},

		// Invalid
		{"invalid cron", true},
		{"* * * * * *", true}, // 6 fields not supported
	}

	for _, tc := range tests {
		triggerType, err := ParseWhen(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseWhen(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseWhen(%q) unexpected error: %v", tc.input, err)
			}
			if triggerType != TriggerCron {
				t.Errorf("ParseWhen(%q) = %v, want TriggerCron", tc.input, triggerType)
			}
		}
	}
}

func TestScheduler_ScheduleAlways(t *testing.T) {
	s := New()
	defer s.Stop()

	cfg := &ScheduleConfig{
		FlowName: "test-flow",
		When:     "always",
		Handler: func(ctx context.Context) error {
			return nil
		},
	}

	err := s.Schedule(cfg)
	if err != nil {
		t.Fatalf("Schedule() unexpected error: %v", err)
	}

	// Should not add any entry for "always" trigger
	if len(s.entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(s.entries))
	}
}

func TestScheduler_ScheduleInterval(t *testing.T) {
	s := New()

	var count int32
	done := make(chan struct{})

	cfg := &ScheduleConfig{
		FlowName: "interval-flow",
		When:     "@every 1s",
		Handler: func(ctx context.Context) error {
			if atomic.AddInt32(&count, 1) >= 2 {
				close(done)
			}
			return nil
		},
	}

	err := s.Schedule(cfg)
	if err != nil {
		t.Fatalf("Schedule() unexpected error: %v", err)
	}

	s.Start()

	// Wait for at least 2 executions or timeout
	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for executions, got %d", atomic.LoadInt32(&count))
	}

	s.Stop()

	executions := atomic.LoadInt32(&count)
	if executions < 2 {
		t.Errorf("expected at least 2 executions, got %d", executions)
	}
}

func TestScheduler_ScheduleCron(t *testing.T) {
	s := New()
	defer s.Stop()

	cfg := &ScheduleConfig{
		FlowName: "cron-flow",
		When:     "0 3 * * *",
		Handler: func(ctx context.Context) error {
			return nil
		},
	}

	err := s.Schedule(cfg)
	if err != nil {
		t.Fatalf("Schedule() unexpected error: %v", err)
	}

	s.Start()

	// Should have one entry
	if len(s.entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(s.entries))
	}

	// Check next run is scheduled
	next, ok := s.GetNextRun("cron-flow")
	if !ok {
		t.Fatal("expected to find next run time")
	}
	if next.IsZero() {
		t.Error("expected non-zero next run time")
	}
}

func TestScheduler_Unschedule(t *testing.T) {
	s := New()
	defer s.Stop()

	cfg := &ScheduleConfig{
		FlowName: "unschedule-flow",
		When:     "@every 1m",
		Handler: func(ctx context.Context) error {
			return nil
		},
	}

	s.Schedule(cfg)
	s.Start()

	if len(s.entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(s.entries))
	}

	s.Unschedule("unschedule-flow")

	if len(s.entries) != 0 {
		t.Errorf("expected 0 entries after unschedule, got %d", len(s.entries))
	}
}

func TestScheduler_Entries(t *testing.T) {
	s := New()
	defer s.Stop()

	handler := func(ctx context.Context) error { return nil }

	s.Schedule(&ScheduleConfig{FlowName: "flow1", When: "@every 1m", Handler: handler})
	s.Schedule(&ScheduleConfig{FlowName: "flow2", When: "@daily", Handler: handler})
	s.Schedule(&ScheduleConfig{FlowName: "flow3", When: "0 0 * * *", Handler: handler})

	s.Start()

	entries := s.Entries()
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	flowNames := make(map[string]bool)
	for _, e := range entries {
		flowNames[e.FlowName] = true
	}

	for _, name := range []string{"flow1", "flow2", "flow3"} {
		if !flowNames[name] {
			t.Errorf("expected entry for %q", name)
		}
	}
}

func TestScheduler_Stats(t *testing.T) {
	s := New()
	defer s.Stop()

	handler := func(ctx context.Context) error { return nil }
	s.Schedule(&ScheduleConfig{FlowName: "flow1", When: "@every 1m", Handler: handler})
	s.Schedule(&ScheduleConfig{FlowName: "flow2", When: "@daily", Handler: handler})

	stats := s.Stats()

	if stats["scheduled_flows"].(int) != 2 {
		t.Errorf("expected 2 scheduled flows, got %v", stats["scheduled_flows"])
	}
	if stats["running"].(bool) != false {
		t.Error("expected running to be false")
	}

	s.Start()

	stats = s.Stats()
	if stats["running"].(bool) != true {
		t.Error("expected running to be true")
	}
}

func TestScheduler_RescheduleReplacesEntry(t *testing.T) {
	s := New()
	defer s.Stop()

	var count1, count2 int32
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	cfg1 := &ScheduleConfig{
		FlowName: "test-flow",
		When:     "@every 1s",
		Handler: func(ctx context.Context) error {
			if atomic.AddInt32(&count1, 1) == 1 {
				close(done1)
			}
			return nil
		},
	}

	cfg2 := &ScheduleConfig{
		FlowName: "test-flow", // Same name
		When:     "@every 1s",
		Handler: func(ctx context.Context) error {
			if atomic.AddInt32(&count2, 1) == 1 {
				close(done2)
			}
			return nil
		},
	}

	s.Schedule(cfg1)
	s.Start()

	// Wait for first handler to execute
	select {
	case <-done1:
		// First handler executed
	case <-time.After(2 * time.Second):
		t.Fatal("first handler did not execute in time")
	}

	// Reschedule with new handler
	s.Schedule(cfg2)

	// Wait for second handler to execute
	select {
	case <-done2:
		// Second handler executed
	case <-time.After(2 * time.Second):
		t.Fatal("second handler did not execute in time")
	}

	// Should still have only 1 entry
	if len(s.entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(s.entries))
	}
}

func TestScheduler_StartStop(t *testing.T) {
	s := New()

	if s.IsRunning() {
		t.Error("scheduler should not be running initially")
	}

	s.Start()
	if !s.IsRunning() {
		t.Error("scheduler should be running after Start()")
	}

	// Start again should be idempotent
	s.Start()
	if !s.IsRunning() {
		t.Error("scheduler should still be running after second Start()")
	}

	s.Stop()
	if s.IsRunning() {
		t.Error("scheduler should not be running after Stop()")
	}
}

func TestScheduler_InvalidSchedule(t *testing.T) {
	s := New()
	defer s.Stop()

	cfg := &ScheduleConfig{
		FlowName: "invalid-flow",
		When:     "invalid cron expression",
		Handler: func(ctx context.Context) error {
			return nil
		},
	}

	err := s.Schedule(cfg)
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestScheduler_GetNextRunNotFound(t *testing.T) {
	s := New()
	defer s.Stop()

	_, ok := s.GetNextRun("nonexistent")
	if ok {
		t.Error("expected ok to be false for nonexistent flow")
	}
}

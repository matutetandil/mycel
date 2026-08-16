package scheduler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/metrics"
)

// What a scheduled flow leaves behind.
//
// It has nobody watching it: no caller waiting for an answer, no message to
// acknowledge or reject. How many are scheduled and how the runs went is the
// only trace it leaves, and neither was recorded — the counter was declared,
// registered, exposed and never incremented, and a failed run was printed to
// stdout with fmt.Printf, unstructured, where a log pipeline reading JSON
// would not find it.

func exposed(t *testing.T, registry *metrics.Registry) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return recorder.Body.String()
}

func TestHowManyFlowsAreScheduled(t *testing.T) {
	registry := metrics.NewRegistry("orders", "1.0.0", "2.19.0", "test")
	metrics.SetDefault(registry)

	scheduler := New()
	defer scheduler.Stop()

	for _, flow := range []string{"nightly_import", "hourly_sync"} {
		if err := scheduler.Schedule(&ScheduleConfig{
			FlowName: flow,
			When:     "@every 1h",
			Handler:  func(ctx context.Context) error { return nil },
		}); err != nil {
			t.Fatalf("Schedule %s: %v", flow, err)
		}
	}

	if !strings.Contains(exposed(t, registry), "mycel_scheduled_flows 2") {
		t.Errorf("scheduling two flows did not say so:\n%s", exposed(t, registry))
	}

	// And unscheduling one — which is what a hot reload does to a flow
	// somebody removed — brings it back down. A gauge that only goes up is a
	// gauge that lies after the first reload.
	scheduler.Unschedule("hourly_sync")
	if !strings.Contains(exposed(t, registry), "mycel_scheduled_flows 1") {
		t.Errorf("unscheduling a flow did not say so:\n%s", exposed(t, registry))
	}
}

func TestARunThatHappenedAndOneThatFailed(t *testing.T) {
	registry := metrics.NewRegistry("orders", "1.0.0", "2.19.0", "test")
	metrics.SetDefault(registry)

	scheduler := New()
	defer scheduler.Stop()

	ran := make(chan struct{}, 4)
	shouldFail := false

	if err := scheduler.Schedule(&ScheduleConfig{
		FlowName: "nightly_import",
		// Fast enough that the test does not wait on a clock.
		When: "@every 100ms",
		Handler: func(ctx context.Context) error {
			ran <- struct{}{}
			if shouldFail {
				return errors.New("the supplier's API is down")
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	scheduler.Start()

	waitForRun(t, ran)
	shouldFail = true
	waitForRun(t, ran)
	// Give the recorder a moment after the handler returns.
	time.Sleep(200 * time.Millisecond)

	metricsText := exposed(t, registry)
	if !strings.Contains(metricsText, `mycel_schedule_executed_total{flow="nightly_import",status="success"}`) {
		t.Errorf("a run that happened was not counted:\n%s", metricsText)
	}
	if !strings.Contains(metricsText, `mycel_schedule_executed_total{flow="nightly_import",status="error"}`) {
		t.Errorf("a run that failed was not counted:\n%s", metricsText)
	}
}

func TestAFlowThatIsTriggeredByItsSourceIsNotScheduled(t *testing.T) {
	// `when = "always"` means the connector drives it, so there is nothing to
	// put on a clock — and nothing to count as scheduled.
	registry := metrics.NewRegistry("orders", "1.0.0", "2.19.0", "test")
	metrics.SetDefault(registry)

	scheduler := New()
	defer scheduler.Stop()

	if err := scheduler.Schedule(&ScheduleConfig{
		FlowName: "process_order",
		When:     "always",
		Handler:  func(ctx context.Context) error { return nil },
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if len(scheduler.Entries()) != 0 {
		t.Errorf("a source-driven flow was put on the clock: %v", scheduler.Entries())
	}
}

func TestAScheduleNobodyCanRead(t *testing.T) {
	scheduler := New()
	defer scheduler.Stop()

	err := scheduler.Schedule(&ScheduleConfig{
		FlowName: "nightly_import",
		When:     "every night please",
		Handler:  func(ctx context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("a schedule nobody can read was accepted, so the flow never runs and nothing says why")
	}
	if !strings.Contains(err.Error(), "every night please") && !strings.Contains(err.Error(), "nightly_import") {
		t.Errorf("the error names neither the flow nor the schedule: %v", err)
	}
}

func waitForRun(t *testing.T, ran chan struct{}) {
	t.Helper()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduled flow never ran")
	}
}

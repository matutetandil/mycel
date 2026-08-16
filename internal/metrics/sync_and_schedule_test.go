package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The metrics somebody watches when a service is behaving oddly.
//
// Contention and scheduling are the two things a flow cannot tell you about
// itself: a message that waited four seconds for a lock looks, in the flow
// duration, exactly like a message that took four seconds to process, and a
// scheduled flow that never ran looks like nothing at all.

// scrape reads the metrics the way Prometheus does.
func scrape(t *testing.T, registry *Registry) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("/metrics answered %d", recorder.Code)
	}
	return recorder.Body.String()
}

func TestWaitingForALock(t *testing.T) {
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.RecordLockAcquired("process_order", "flow", 250*time.Millisecond)
	registry.RecordLockAcquired("process_order", "flow", 4*time.Second)
	registry.RecordLockReleased("process_order", "flow")
	registry.RecordLockTimeout("process_order", "flow", 30*time.Second)

	exposed := scrape(t, registry)

	for _, want := range []string{
		`mycel_lock_acquired_total{flow="process_order",purpose="flow"} 2`,
		`mycel_lock_released_total{flow="process_order",purpose="flow"} 1`,
		`mycel_lock_timeout_total{flow="process_order",purpose="flow"} 1`,
	} {
		if !strings.Contains(exposed, want) {
			t.Errorf("/metrics does not carry %q", want)
		}
	}
	// The wait is the number that matters: time spent waiting for a lock is
	// invisible in the flow duration, so a consumer that looks slow and a
	// consumer that is queueing look identical without it.
	if !strings.Contains(exposed, "mycel_lock_wait_seconds") {
		t.Errorf("nothing says how long anything waited:\n%s", exposed)
	}
}

func TestWaitingForAPermit(t *testing.T) {
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.RecordSemaphoreAcquired("call_supplier", 100*time.Millisecond)
	registry.RecordSemaphoreReleased("call_supplier")
	registry.RecordSemaphoreTimeout("call_supplier", 30*time.Second)
	registry.SetSemaphoreAvailable("call_supplier", 3)

	exposed := scrape(t, registry)

	for _, want := range []string{
		`mycel_semaphore_acquired_total{flow="call_supplier"} 1`,
		`mycel_semaphore_released_total{flow="call_supplier"} 1`,
		`mycel_semaphore_timeout_total{flow="call_supplier"} 1`,
		`mycel_semaphore_available{flow="call_supplier"} 3`,
	} {
		if !strings.Contains(exposed, want) {
			t.Errorf("/metrics does not carry %q", want)
		}
	}
}

func TestWaitingForAnotherFlow(t *testing.T) {
	// Coordinate: one flow waits for another to signal. A wait that never
	// completes is the failure, and it is only visible as the difference
	// between these two counters.
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.RecordCoordinateSignal("import_parent")
	registry.RecordCoordinateWait("import_child")
	registry.RecordCoordinateWaitComplete("import_child", 2*time.Second, true)
	registry.RecordCoordinateWaitComplete("import_child", 30*time.Second, false)
	registry.RecordCoordinatePreflightHit("import_child")

	exposed := scrape(t, registry)

	for _, want := range []string{
		`mycel_coordinate_signal_total{flow="import_parent"} 1`,
		`mycel_coordinate_wait_total{flow="import_child"} 1`,
		`mycel_coordinate_timeout_total{flow="import_child"} 1`,
		// The preflight is what skips a wait whose condition already holds,
		// and its rate is how you tell a coordinate that is helping from one
		// that is only costing.
		`mycel_coordinate_preflight_hit_total{flow="import_child"} 1`,
	} {
		if !strings.Contains(exposed, want) {
			t.Errorf("/metrics does not carry %q", want)
		}
	}
}

func TestWhatAScheduledFlowLeavesBehind(t *testing.T) {
	// A scheduled flow has nobody watching it: no caller waiting for an
	// answer, no message to nack. How many are scheduled and how they went is
	// the only trace it leaves, and neither was ever recorded.
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.SetScheduledFlows(3)
	registry.RecordScheduleExecution("nightly_import", "success")
	registry.RecordScheduleExecution("nightly_import", "success")
	registry.RecordScheduleExecution("nightly_import", "error")

	exposed := scrape(t, registry)

	if !strings.Contains(exposed, "mycel_scheduled_flows 3") {
		t.Errorf("nothing says how many flows are scheduled:\n%s", exposed)
	}
	for _, want := range []string{
		`mycel_schedule_executed_total{flow="nightly_import",status="success"} 2`,
		`mycel_schedule_executed_total{flow="nightly_import",status="error"} 1`,
	} {
		if !strings.Contains(exposed, want) {
			t.Errorf("/metrics does not carry %q", want)
		}
	}
}

func TestWhatEachConnectorIsDoing(t *testing.T) {
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.RecordConnectorOperation("orders_db", "database", "SELECT", "success", 15*time.Millisecond)
	registry.RecordConnectorOperation("orders_db", "database", "INSERT", "error", time.Second)
	registry.SetConnectorHealth("orders_db", "database", true)
	registry.RecordUndispatchedMessage("rabbit", "rabbitmq", "orders", "order.created")

	exposed := scrape(t, registry)

	for _, want := range []string{
		`mycel_connector_operations_total{connector="orders_db"`,
		`mycel_connector_health{connector="orders_db",type="database"} 1`,
		// A message a consumer received and no flow claimed is silent
		// otherwise: it is acknowledged and gone.
		`mycel_messages_undispatched_total{connector="rabbit"`,
	} {
		if !strings.Contains(exposed, want) {
			t.Errorf("/metrics does not carry %q:\n%s", want, exposed)
		}
	}
}

func TestHowMuchACacheIsHolding(t *testing.T) {
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.SetCacheSize("products", 1500)
	registry.RecordCacheHit("products")
	registry.RecordCacheMiss("products")

	exposed := scrape(t, registry)

	if !strings.Contains(exposed, `mycel_cache_size{cache="products"} 1500`) {
		t.Errorf("nothing says how much the cache holds:\n%s", exposed)
	}
	if !strings.Contains(exposed, `mycel_cache_hits_total{cache="products"} 1`) {
		t.Error("cache hits are not exposed")
	}
}

func TestAMessageDroppedOnPurpose(t *testing.T) {
	// A filter, an accept gate, a dedupe or a sequence guard turning a
	// message away is not an error and must not be counted as one — but it
	// has to be counted, or a flow that drops everything looks like a flow
	// with no traffic.
	registry := NewRegistry("orders", "1.0.0", "2.19.0", "test")

	registry.RecordFlowDrop("process_order", "filter")
	registry.RecordFlowDrop("process_order", "dedupe")
	registry.RecordFlowError("process_order", "write_failed")

	exposed := scrape(t, registry)

	for _, want := range []string{
		`mycel_flow_drops_total{flow="process_order",reason="filter"} 1`,
		`mycel_flow_drops_total{flow="process_order",reason="dedupe"} 1`,
		`mycel_flow_errors_total{error_type="write_failed",flow="process_order"} 1`,
	} {
		if !strings.Contains(exposed, want) {
			t.Errorf("/metrics does not carry %q:\n%s", want, exposed)
		}
	}
}

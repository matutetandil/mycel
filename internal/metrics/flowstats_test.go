package metrics

import (
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func scrapeText(t *testing.T, r *Registry) string {
	t.Helper()

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

// gaugeValue pulls a single labelled sample out of the exposition text.
func gaugeValue(t *testing.T, out, metric, flow string) float64 {
	t.Helper()

	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(metric) + `\{flow="` + regexp.QuoteMeta(flow) + `"\} (\S+)$`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("%s{flow=%q} not found in:\n%s", metric, flow, filterLines(out, "mycel_flow"))
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("parsing %s: %v", m[1], err)
	}
	return v
}

func filterLines(s, sub string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// The point of these gauges: a histogram gives buckets, not the exact fastest
// and slowest execution.
func TestFlowStats_FastestSlowestAverage(t *testing.T) {
	r := NewRegistry("test", "1", "1", "test")

	for _, d := range []time.Duration{7 * time.Millisecond, 3 * time.Millisecond, 200 * time.Millisecond} {
		r.RecordFlowExecution("sync_orders", "success", d)
	}

	out := scrapeText(t, r)
	if got := gaugeValue(t, out, "mycel_flow_duration_fastest_seconds", "sync_orders"); got != 0.003 {
		t.Errorf("fastest = %v, want 0.003", got)
	}
	if got := gaugeValue(t, out, "mycel_flow_duration_slowest_seconds", "sync_orders"); got != 0.2 {
		t.Errorf("slowest = %v, want 0.2", got)
	}
	want := (0.007 + 0.003 + 0.2) / 3
	if got := gaugeValue(t, out, "mycel_flow_duration_average_seconds", "sync_orders"); got != want {
		t.Errorf("average = %v, want %v", got, want)
	}
}

// A flow that fails in 1ms must not take the "fastest" spot or pull the average
// down — a broken service should not look quick.
func TestFlowStats_IgnoresFailedExecutions(t *testing.T) {
	r := NewRegistry("test", "1", "1", "test")

	r.RecordFlowExecution("sync_orders", "success", 50*time.Millisecond)
	r.RecordFlowExecution("sync_orders", "error", 1*time.Millisecond)
	r.RecordFlowExecution("sync_orders", "error", 900*time.Millisecond)

	out := scrapeText(t, r)
	if got := gaugeValue(t, out, "mycel_flow_duration_fastest_seconds", "sync_orders"); got != 0.05 {
		t.Errorf("fastest = %v, want 0.05 (the failed 1ms run must not count)", got)
	}
	if got := gaugeValue(t, out, "mycel_flow_duration_slowest_seconds", "sync_orders"); got != 0.05 {
		t.Errorf("slowest = %v, want 0.05 (the failed 900ms run must not count)", got)
	}

	// The histogram still sees every execution; only these gauges are filtered.
	if !strings.Contains(out, `mycel_flow_duration_seconds_count{flow="sync_orders"} 3`) {
		t.Error("the existing histogram should still observe failed executions")
	}
}

// Throughput divides by the time actually elapsed, not a flat 60, or a service
// coming up would report a fraction of its real rate.
func TestFlowStats_ThroughputUsesElapsedTimeOnStartup(t *testing.T) {
	r := NewRegistry("test", "1", "1", "test")

	for i := 0; i < 10; i++ {
		r.RecordFlowExecution("sync_orders", "success", time.Millisecond)
	}

	// The registry was created moments ago, so the divisor floors at 1 second
	// and all ten land in the window.
	if got := gaugeValue(t, scrapeText(t, r), "mycel_flow_messages_per_second", "sync_orders"); got != 10 {
		t.Errorf("messages per second = %v, want 10", got)
	}
}

// A flow that goes quiet must decay to zero rather than reporting whatever it
// was doing a minute ago.
func TestFlowStats_QuietFlowDecays(t *testing.T) {
	fs := NewFlowStats()
	fs.Observe("sync_orders", time.Millisecond)

	fs.mu.Lock()
	st := fs.flows["sync_orders"]
	// Pretend the last write was more than a full window ago.
	st.advance(st.lastSec + throughputWindow + 5)
	var remaining uint32
	for _, n := range st.buckets {
		remaining += n
	}
	fs.mu.Unlock()

	if remaining != 0 {
		t.Errorf("expected the ring to clear after a full window of silence, got %d", remaining)
	}
}

// Flows with no successful executions must not appear at all, rather than
// reporting a fastest and slowest of zero.
func TestFlowStats_NoSuccessesEmitsNothing(t *testing.T) {
	r := NewRegistry("test", "1", "1", "test")
	r.RecordFlowExecution("always_fails", "error", time.Millisecond)

	if out := scrapeText(t, r); strings.Contains(out, "mycel_flow_duration_fastest_seconds") {
		t.Errorf("a flow with no successes should emit no timing gauges:\n%s", filterLines(out, "fastest"))
	}
}

// A drop short-circuits before the transform, so it is the fastest thing a
// flow ever does. Counting it would let a consumer that filters out most of
// its input own the "fastest" gauge permanently and report a throughput it
// never achieved.
func TestFlowStats_IgnoresDroppedExecutions(t *testing.T) {
	r := NewRegistry("test", "1", "1", "test")

	r.RecordFlowExecution("sync_orders", FlowStatusSuccess, 50*time.Millisecond)
	for i := 0; i < 8; i++ {
		r.RecordFlowExecution("sync_orders", FlowStatusDropped, 20*time.Microsecond)
	}

	out := scrapeText(t, r)
	if got := gaugeValue(t, out, "mycel_flow_duration_fastest_seconds", "sync_orders"); got != 0.05 {
		t.Errorf("fastest = %v, want 0.05 (the 8 drops must not count)", got)
	}
	if got := gaugeValue(t, out, "mycel_flow_duration_average_seconds", "sync_orders"); got != 0.05 {
		t.Errorf("average = %v, want 0.05", got)
	}

	// Drops are still visible, just as their own status rather than as success.
	for _, want := range []string{
		`mycel_flow_executions_total{flow="sync_orders",status="dropped"} 8`,
		`mycel_flow_executions_total{flow="sync_orders",status="success"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, filterLines(out, "mycel_flow_executions"))
		}
	}
}

// The drop counter breaks down by gate, matching the `reason` on the log line,
// so the two can be read together.
func TestRecordFlowDrop_ByReason(t *testing.T) {
	r := NewRegistry("test", "1", "1", "test")

	r.RecordFlowDrop("sync_orders", "filter")
	r.RecordFlowDrop("sync_orders", "filter")
	r.RecordFlowDrop("sync_orders", "dedupe_match")

	out := scrapeText(t, r)
	for _, want := range []string{
		`mycel_flow_drops_total{flow="sync_orders",reason="filter"} 2`,
		`mycel_flow_drops_total{flow="sync_orders",reason="dedupe_match"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, filterLines(out, "mycel_flow_drops"))
		}
	}
}

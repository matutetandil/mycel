package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Following a message through a flow.
//
// This is what `--verbose-flow` prints and what the debugger reads: every
// stage a message passed, what it looked like at each, and where it stopped.
// It is only ever used when somebody is trying to work out why a flow did
// something, so a stage missing from it is the one they cannot see.

func TestEveryStageIsPrintedUnderAName(t *testing.T) {
	// The renderer is the whole point of the trace, and a stage falling
	// through to its raw name — "validate_input" rather than VALIDATE INPUT —
	// is how somebody discovers a stage nobody labelled.
	renderer := &Renderer{}

	for stage, want := range map[Stage]string{
		StageInput:       "INPUT",
		StageSanitize:    "SANITIZE",
		StageFilter:      "FILTER",
		StageAccept:      "ACCEPT",
		StageDedupe:      "DEDUPE",
		StageValidateIn:  "VALIDATE INPUT",
		StageEnrich:      "ENRICH",
		StageTransform:   "TRANSFORM",
		StageStep:        "STEP",
		StageValidateOut: "VALIDATE OUTPUT",
		StageRead:        "READ",
		StageWrite:       "WRITE",
		StageCacheHit:    "CACHE HIT",
		StageCacheMiss:   "CACHE MISS",
	} {
		if got := renderer.stageLabel(Event{Stage: stage}); got != want {
			t.Errorf("%s reads as %q, want %q", stage, got, want)
		}
	}

	// Stages that name something say which one: which connector was written
	// to, which step ran, which enrichment was applied.
	for _, tc := range []struct {
		event Event
		want  string
	}{
		{Event{Stage: StageWrite, Name: "orders_db"}, "WRITE → orders_db"},
		{Event{Stage: StageRead, Name: "orders_db"}, "READ → orders_db"},
		{Event{Stage: StageStep, Name: "fetch_customer"}, "STEP (fetch_customer)"},
		{Event{Stage: StageEnrich, Name: "customer"}, "ENRICH (customer)"},
	} {
		if got := renderer.stageLabel(tc.event); got != tc.want {
			t.Errorf("%+v reads as %q, want %q", tc.event, got, tc.want)
		}
	}

	// Anything unrecognised is shown rather than swallowed.
	if got := renderer.stageLabel(Event{Stage: Stage("something_new")}); got != "something_new" {
		t.Errorf("an unknown stage reads as %q", got)
	}
}

func TestStagesAreLoggedAsTheyHappen(t *testing.T) {
	// The collector behind --verbose-flow. Its point is that the line appears
	// while the flow is running, so a flow that hangs shows where.
	var written bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&written, &slog.HandlerOptions{Level: slog.LevelDebug}))
	collector := NewLogCollector(logger)

	collector.Record(Event{
		Stage:    StageTransform,
		Name:     "normalise",
		Duration: 12 * time.Millisecond,
		Output:   map[string]interface{}{"order_id": "order-1"},
		Detail:   "3 rules",
	})

	line := written.String()
	for _, want := range []string{"transform", "normalise", "3 rules", "order-1"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log line does not mention %q: %s", want, line)
		}
	}

	// And it is kept, so the whole trace can be rendered at the end as well.
	if events := collector.Events(); len(events) != 1 || events[0].Name != "normalise" {
		t.Errorf("events = %v", events)
	}
}

func TestAStageThatFailedIsLoudEnoughToSee(t *testing.T) {
	// A failure logged at debug alongside everything else is a failure nobody
	// reads: the level is what separates it from the running commentary.
	var written bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&written, &slog.HandlerOptions{Level: slog.LevelWarn}))
	collector := NewLogCollector(logger)

	collector.Record(Event{Stage: StageWrite, Name: "orders_db", Error: errors.New("connection refused")})
	collector.Record(Event{Stage: StageTransform, Name: "normalise"})

	line := written.String()
	if !strings.Contains(line, "connection refused") {
		t.Errorf("the failure was not logged: %s", line)
	}
	if strings.Contains(line, "normalise") {
		t.Errorf("an ordinary stage was logged at the same level as a failure: %s", line)
	}
}

func TestStagesThatDidNotRunSayWhy(t *testing.T) {
	var written bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&written, &slog.HandlerOptions{Level: slog.LevelDebug}))
	collector := NewLogCollector(logger)

	// A skipped stage is the answer to "why did nothing happen" — a filter
	// that rejected the message, a dedupe that had seen it before.
	collector.Record(Event{Stage: StageDedupe, Skipped: true, Detail: "already processed"})
	// A dry run is a flow that was traced without being allowed to write.
	collector.Record(Event{Stage: StageWrite, Name: "orders_db", DryRun: true})

	line := written.String()
	for _, want := range []string{"skipped", "already processed", "dry_run"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log does not show %q: %s", want, line)
		}
	}
}

func TestAPayloadIsCutDownBeforeItIsLogged(t *testing.T) {
	// A trace line carrying a whole product catalogue is a log nobody can
	// read and a bill nobody expected.
	big := map[string]interface{}{"description": strings.Repeat("x", 500)}

	shown := truncateJSON(big, 200)
	if len(shown) > 210 {
		t.Errorf("a large payload was logged whole: %d characters", len(shown))
	}
	if !strings.HasSuffix(shown, "...") {
		t.Errorf("nothing says the payload was cut: %q", shown[len(shown)-10:])
	}

	// Something small goes through as it is, and as valid JSON.
	small := truncateJSON(map[string]interface{}{"id": "order-1"}, 200)
	var back map[string]interface{}
	if err := json.Unmarshal([]byte(small), &back); err != nil || back["id"] != "order-1" {
		t.Errorf("a small payload came out as %q", small)
	}

	// And something that cannot be JSON at all is still shown rather than
	// dropped: the trace exists to say what was there.
	if got := truncateJSON(make(chan int), 200); got == "" {
		t.Error("a value that cannot be encoded was logged as nothing")
	}
}

func TestRecordingAStageWithNoFunctionToWrap(t *testing.T) {
	// Used where a stage is not a call — a message arriving, a cache hit.
	collector := &MemoryCollector{}
	ctx := WithTrace(context.Background(), &Context{Collector: collector})

	RecordSimple(ctx, StageCacheHit, "orders_cache", map[string]interface{}{"id": "order-1"}, "60s left")

	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
	if events[0].Stage != StageCacheHit || events[0].Detail != "60s left" {
		t.Errorf("event = %+v", events[0])
	}

	// Nothing is recorded when nobody is tracing, which is every request in
	// production: this runs on the hot path.
	RecordSimple(context.Background(), StageCacheHit, "orders_cache", nil, "")
	RecordSkipped(context.Background(), StageDedupe, "", "")
}

func TestWhatIsRecordedIsACopy(t *testing.T) {
	// A trace holds what the message looked like at that stage. Holding the
	// message itself instead means every stage shows the final version, and
	// the trace answers "what did the transform change" with "nothing".
	payload := map[string]interface{}{"status": "pending"}

	kept, ok := Snapshot(payload).(map[string]interface{})
	if !ok {
		t.Fatalf("snapshot = %T", Snapshot(payload))
	}
	payload["status"] = "paid"

	if kept["status"] != "pending" {
		t.Errorf("the recorded value changed underneath: %v", kept)
	}

	// Rows too, which is what a read stage records.
	rows := []map[string]interface{}{{"status": "pending"}}
	keptRows, ok := Snapshot(rows).([]map[string]interface{})
	if !ok {
		t.Fatalf("snapshot = %T", Snapshot(rows))
	}
	rows[0]["status"] = "paid"
	if keptRows[0]["status"] != "pending" {
		t.Errorf("the recorded rows changed underneath: %v", keptRows)
	}

	if Snapshot(nil) != nil {
		t.Error("nothing was recorded as something")
	}
}

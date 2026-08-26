package examples

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The two-step half of the async-jobs example.
//
// Its README can only show `curl /jobs/$JOB_ID`, because the id comes from the
// answer to the request before it — and the harness runs each command on its
// own. So the chaining is asserted here: submit, read the id back, poll it
// until the background run is finished, and check that what the flow produced
// is what the job reports.
func TestTheAsyncExampleFinishesInTheBackground(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "async-jobs")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	status, answer := svc.run(t, fmt.Sprintf(
		`curl -X POST %s/reports -H 'Content-Type: application/json' -d '{"month":"2026-08"}'`, base))
	if status != 202 {
		t.Fatalf("submitting answered %d, want 202 Accepted: %s", status, answer)
	}

	var accepted struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(answer), &accepted); err != nil {
		t.Fatalf("the 202 was not JSON: %v: %s", err, answer)
	}
	if accepted.JobID == "" {
		t.Fatalf("no job id to poll with: %s", answer)
	}
	if accepted.Status != "pending" {
		t.Errorf("job reported %q on acceptance, want pending", accepted.Status)
	}

	// GET /jobs/:job_id is registered by the runtime because a flow declares
	// async — no flow in the example serves it.
	var last string
	for attempt := 0; attempt < 40; attempt++ {
		pollStatus, body := svc.run(t, fmt.Sprintf(`curl %s/jobs/%s`, base, accepted.JobID))
		if pollStatus != 200 {
			t.Fatalf("polling answered %d: %s", pollStatus, body)
		}
		last = body

		var job struct {
			Status string          `json:"status"`
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(body), &job); err != nil {
			t.Fatalf("job status was not JSON: %v: %s", err, body)
		}

		switch job.Status {
		case "completed":
			if len(job.Result) == 0 {
				t.Errorf("job completed with no result: %s", body)
			}
			if !strings.Contains(string(job.Result), `"affected"`) {
				t.Errorf("the job result is not what the flow wrote: %s", job.Result)
			}
			return
		case "failed":
			t.Fatalf("the background run failed: %s", job.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the job never finished; last answer: %s", last)
}

// A retry carrying the same key must not write a second row.
//
// The README shows the two requests and their identical answers, but not the
// part that matters — that only one order exists afterwards. A flow could
// answer from the cache and still have written twice.
func TestTheIdempotentFlowWritesOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "async-jobs")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	place := func(key string) string {
		t.Helper()
		command := fmt.Sprintf(
			`curl -X POST %s/orders -H 'Content-Type: application/json' -d '{"customer":"ACME","total":120.50}'`, base)
		if key != "" {
			command = strings.Replace(command, "-H 'Content-Type",
				fmt.Sprintf("-H 'Idempotency-Key: %s' -H 'Content-Type", key), 1)
		}
		status, answer := svc.run(t, command)
		if status < 200 || status >= 300 {
			t.Fatalf("placing an order answered %d: %s", status, answer)
		}
		return answer
	}

	first := place("order-777")
	again := place("order-777")
	if first != again {
		t.Errorf("the retry got a different answer:\n  first: %s\n  again: %s", first, again)
	}

	// No key: not idempotent, and a row of its own.
	other := place("")
	if other == first {
		t.Errorf("a request with no key reused the stored answer: %s", other)
	}

	status, listed := svc.run(t, fmt.Sprintf(`curl %s/orders`, base))
	if status != 200 {
		t.Fatalf("listing answered %d: %s", status, listed)
	}
	var orders []map[string]interface{}
	if err := json.Unmarshal([]byte(listed), &orders); err != nil {
		t.Fatalf("the listing was not JSON: %v: %s", err, listed)
	}
	if len(orders) != 2 {
		t.Errorf("%d orders were written, want 2 — the retry wrote a second one", len(orders))
	}
}

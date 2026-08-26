package rest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// A message a gate dropped is answered in Mycel's words, not in Go's.
//
// The value a drop produces is an internal struct and it went out as one:
// {"Filtered":true,"Policy":"ack","MessageID":"","MaxRequeue":0,...}. Two
// things wrong with that. The caller reads Go field names and fields that mean
// nothing over HTTP, so renaming one would change the API without anybody
// deciding to; and Detail carries the expression that rejected them, which
// hands a refused client the rule it failed.
func TestADroppedMessageAnswersWithoutTheInternals(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.RegisterRoute("POST /orders", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return &flow.FilteredResultWithPolicy{
			Filtered:   true,
			Policy:     "ack",
			MaxRequeue: 3,
			Reason:     "accept",
			Detail:     "input.tenant == 'acme'",
		}, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("POST", "/orders", strings.NewReader(`{"tenant":"other"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("status = %d, want 200 — a drop is a decision, not a failure", rr.Code)
	}

	body := rr.Body.String()
	for _, leaked := range []string{"Filtered", "MaxRequeue", "MessageID", "Policy", "Detail", "input.tenant"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the answer carries %q: %s", leaked, body)
		}
	}

	var answer map[string]interface{}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("the answer was not JSON: %v: %s", err, body)
	}
	if answer["status"] != "dropped" {
		t.Errorf("status = %v, want dropped: %s", answer["status"], body)
	}
	if answer["reason"] != "accept" {
		t.Errorf("reason = %v, want the gate that decided: %s", answer["reason"], body)
	}
}

// Anything that is not a drop goes out untouched.
func TestANormalAnswerIsNotRewritten(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.RegisterRoute("POST /orders", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "abc", "status": "created"}, nil
	})
	conn.setupRoutes()

	req := httptest.NewRequest("POST", "/orders", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	conn.mux.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), `"id":"abc"`) {
		t.Errorf("the answer was rewritten: %s", rr.Body.String())
	}
}

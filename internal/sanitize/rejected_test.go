package sanitize

import (
	"errors"
	"strings"
	"testing"
)

// Every way the pipeline turns input away has to be recognisable as a
// rejection.
//
// Callers decide the HTTP status from this: a rejection is the sender's
// problem (400), anything else is ours (500). Reported as a 500, an oversized
// field became retryable — so the client, and every load balancer between,
// kept re-sending a payload that could never be accepted.
func TestEveryRejectionIsMarkedAsOne(t *testing.T) {
	pipeline := NewPipeline(&Config{
		MaxInputLength: 200,
		MaxFieldLength: 50,
		MaxFieldDepth:  3,
	})

	deep := map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"c": map[string]interface{}{"d": 1}}}}

	for name, input := range map[string]map[string]interface{}{
		"input over the total size":  {"field": strings.Repeat("x", 300)},
		"a field over its length":    {"field": strings.Repeat("x", 80)},
		"nesting past the max depth": deep,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pipeline.Sanitize(input)
			if err == nil {
				t.Fatal("input was accepted")
			}
			if !errors.Is(err, ErrRejected) {
				t.Errorf("rejection not marked as one: %v", err)
			}
		})
	}
}

// Input the pipeline is happy with comes back untouched and unmarked.
func TestAcceptedInputIsNotMarkedRejected(t *testing.T) {
	pipeline := NewPipeline(&Config{
		MaxInputLength: 4096,
		MaxFieldLength: 512,
		MaxFieldDepth:  10,
	})

	out, err := pipeline.Sanitize(map[string]interface{}{"name": "bob", "n": 3})
	if err != nil {
		t.Fatalf("clean input rejected: %v", err)
	}
	if out["name"] != "bob" {
		t.Errorf("name = %v, want bob", out["name"])
	}
}

package connector

import (
	"context"
	"testing"
)

// Headers a single request carries, as opposed to the ones a connector sends
// on every request. They travel on the context, since every ability a
// connector has — Read, Write, Call — takes one, and none of them takes a
// header map.
func TestRequestHeadersTravelOnTheContext(t *testing.T) {
	ctx := context.Background()
	if got := RequestHeaders(ctx); got != nil {
		t.Errorf("a bare context carries headers: %v", got)
	}

	ctx = WithRequestHeaders(ctx, map[string]string{"Store": "au"})
	if got := RequestHeaders(ctx); got["Store"] != "au" {
		t.Errorf("headers = %v", got)
	}

	// Nothing to add leaves the context as it was.
	if again := WithRequestHeaders(ctx, nil); RequestHeaders(again)["Store"] != "au" {
		t.Error("an empty set replaced the headers already there")
	}

	// A later set replaces an earlier one: a step's headers are its own.
	inner := WithRequestHeaders(ctx, map[string]string{"Locale": "en"})
	if got := RequestHeaders(inner); got["Locale"] != "en" || got["Store"] != "" {
		t.Errorf("inner headers = %v, want only the later set", got)
	}
}

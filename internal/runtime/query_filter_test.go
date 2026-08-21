package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A filter document says which documents a flow wants.
//
// The documentation writes one as query_filter = { order_id = "input.order_id" }
// and the examples as { "_id" = ":id" } — the two ways everything else in Mycel
// refers to something the message carries. Neither was resolved: the document
// reached the driver as written, so it matched documents whose field literally
// held the text "input.order_id", which is to say none.
//
// On a read it was worse than that: nothing consulted it at all, so a flow
// asking for the active users answered with every user. Verified against a real
// Mongo before the fix, with one active and one inactive document.

func filterHandler(t *testing.T) *FlowHandler {
	t.Helper()
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return &FlowHandler{
		Config:      &flow.Config{Name: "search"},
		Connectors:  connector.NewRegistry(),
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestAFilterTakesItsValuesFromTheMessage(t *testing.T) {
	h := filterHandler(t)

	got, err := h.resolveFilterDocument(context.Background(), map[string]interface{}{
		"order_id": "input.order_id",
		"status":   "active",
		"_id":      ":id",
	}, map[string]interface{}{
		"order_id": "ord-1",
		"id":       "abc123",
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	if got["order_id"] != "ord-1" {
		t.Errorf(`order_id resolved to %#v, want the message's value`, got["order_id"])
	}
	if got["_id"] != "abc123" {
		t.Errorf(`":id" resolved to %#v, want the path parameter`, got["_id"])
	}
	if got["status"] != "active" {
		t.Errorf("a constant became %#v", got["status"])
	}
}

func TestAnOperatorInsideAFilterIsResolvedToo(t *testing.T) {
	// { created_at = { "$gte" = "input.since" } } — the operator is the
	// operator, and what it compares against comes from the message.
	h := filterHandler(t)

	got, err := h.resolveFilterDocument(context.Background(), map[string]interface{}{
		"created_at": map[string]interface{}{"$gte": "input.since"},
		"name":       map[string]interface{}{"$regex": "input.q", "$options": "i"},
	}, map[string]interface{}{
		"since": "2026-01-01",
		"q":     "ann",
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	created, _ := got["created_at"].(map[string]interface{})
	if created["$gte"] != "2026-01-01" {
		t.Errorf("$gte resolved to %#v", created["$gte"])
	}
	name, _ := got["name"].(map[string]interface{})
	if name["$regex"] != "ann" {
		t.Errorf("$regex resolved to %#v", name["$regex"])
	}
	if name["$options"] != "i" {
		t.Errorf("$options became %#v; it is not an expression", name["$options"])
	}
}

func TestAFilterBuildsItsOwnEvaluator(t *testing.T) {
	// A read flow may declare no transform, and then nothing has built the
	// evaluator — without it every expression is left as the text it is, and
	// the filter matches documents holding that text. Found exactly that way:
	// the search returned nothing at all.
	h := filterHandler(t)
	h.Transformer = nil

	got, err := h.resolveFilterDocument(context.Background(),
		map[string]interface{}{"name": "input.q"},
		map[string]interface{}{"q": "ann"})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if got["name"] != "ann" {
		t.Errorf("with no transform block the filter resolved to %#v", got["name"])
	}
}

func TestAFilterOnAListIsResolvedElementByElement(t *testing.T) {
	h := filterHandler(t)

	got, err := h.resolveFilterDocument(context.Background(), map[string]interface{}{
		"$or": []interface{}{
			map[string]interface{}{"email": "input.email"},
			map[string]interface{}{"name": "input.name"},
		},
	}, map[string]interface{}{"email": "a@b.com", "name": "Ann"})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	alternatives, _ := got["$or"].([]interface{})
	if len(alternatives) != 2 {
		t.Fatalf("$or has %d alternatives", len(alternatives))
	}
	first, _ := alternatives[0].(map[string]interface{})
	if first["email"] != "a@b.com" {
		t.Errorf("the first alternative resolved to %#v", first)
	}
}

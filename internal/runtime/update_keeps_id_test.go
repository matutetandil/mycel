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

// The recordingWriter in multi_destination_test.go already keeps what a
// destination was handed; lastWrite reads the one write these tests make.
func lastWrite(t *testing.T, w *recordingWriter) *connector.Data {
	t.Helper()
	writes := w.writes()
	if len(writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(writes))
	}
	return writes[0]
}

func newUpdateHandler(t *testing.T, cfg *flow.Config) (*FlowHandler, *recordingWriter) {
	t.Helper()
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}
	dest := &recordingWriter{name: "db"}
	return &FlowHandler{
		Config:      cfg,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, dest
}

// An update addresses a row by its id, so the id is a filter and not a column
// to set. It used to be deleted from the INPUT to keep it out of the payload,
// which also took it away from everything downstream that reads input — a
// transform, a step's params, and the ${input.id} in an after { invalidate },
// which is the pattern the documentation shows for exactly this verb.
//
// On a read the same reference resolves, so the two behaved differently for no
// reason an author could see, and a step naming it failed with
// "no such key: id".
func TestHandleUpdate_InputKeepsTheIDForEverythingDownstream(t *testing.T) {
	h, dest := newUpdateHandler(t, &flow.Config{
		Name: "update_product",
		To:   &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "products"}},
	})

	input := map[string]interface{}{"id": "7", "name": "Renamed"}
	if _, err := h.handleUpdate(context.Background(), input, dest); err != nil {
		t.Fatalf("handleUpdate: %v", err)
	}

	if input["id"] != "7" {
		t.Errorf("the id was taken out of the input; a step, a transform or ${input.id} in an after block cannot see it")
	}
}

// And it still does not reach the SET clause, which is why it was being
// deleted in the first place.
func TestHandleUpdate_IDIsAFilterNotAColumn(t *testing.T) {
	h, dest := newUpdateHandler(t, &flow.Config{
		Name: "update_product",
		To:   &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "products"}},
	})

	input := map[string]interface{}{"id": "7", "name": "Renamed"}
	if _, err := h.handleUpdate(context.Background(), input, dest); err != nil {
		t.Fatalf("handleUpdate: %v", err)
	}

	written := lastWrite(t, dest)
	payload := written.Payload
	if _, present := payload["id"]; present {
		t.Error("the id reached the payload; an update would try to set the column it is addressing")
	}
	if payload["name"] != "Renamed" {
		t.Errorf("payload = %#v", payload)
	}
	if written.Filters["id"] != "7" {
		t.Errorf("filters = %#v, want the id", written.Filters)
	}
}

// A flow that states what to write owns its payload — with the id back in
// scope, naming it there is now something an author can do, and nothing should
// take it away again.
func TestHandleUpdate_ATransformMayNameTheID(t *testing.T) {
	h, dest := newUpdateHandler(t, &flow.Config{
		Name: "update_product",
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{
				"name":       "input.name",
				"updated_by": "'id-' + input.id",
			},
		},
		To: &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "products"}},
	})

	input := map[string]interface{}{"id": "7", "name": "Renamed"}
	if _, err := h.handleUpdate(context.Background(), input, dest); err != nil {
		t.Fatalf("handleUpdate: %v", err)
	}

	payload := lastWrite(t, dest).Payload
	if payload["updated_by"] != "id-7" {
		t.Errorf("a transform could not read input.id: payload = %#v", payload)
	}
}

// The end of the same thread: an after block on an update resolves ${input.id}
// in its keys. This is the example the caching guide shows, and it used to
// build "product:" — an unresolvable path renders as the empty string — so the
// entry the write had just made stale survived, silently, and the flow
// reported success. The example in this repository invalidates a static key
// instead, which is what working around it looks like.
func TestHandleUpdate_InvalidationKeySeesTheID(t *testing.T) {
	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage: "redis",
		Keys:    []string{"product:${input.id}"},
	})
	defer done()

	h.Config.To = &flow.ToConfig{Connector: "db", ConnectorParams: map[string]interface{}{"target": "products"}}
	dest := &recordingWriter{name: "db"}

	ctx := ctxWithSteps(nil, nil)
	for _, k := range []string{"product:7", "product:", "product:8"} {
		_ = cache.Set(ctx, k, []byte("x"), 0)
	}

	input := map[string]interface{}{"id": "7", "name": "Renamed"}
	if _, err := h.handleUpdate(ctx, input, dest); err != nil {
		t.Fatalf("handleUpdate: %v", err)
	}
	if err := h.executeInvalidation(ctx, input, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}

	// product:7 is dropped; the empty-suffix key the old behaviour would have
	// aimed at is left alone.
	if left := remaining(t, cache, "product:7", "product:", "product:8"); len(left) != 2 ||
		left[0] != "product:" || left[1] != "product:8" {
		t.Errorf("remaining = %v, want product:7 dropped and the others left", left)
	}
}

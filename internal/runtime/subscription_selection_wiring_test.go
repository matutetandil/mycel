package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// The runtime is what carries a flow's `selection` to the connector: the
// connector only ever sees an operation name and a handler, so without this
// call the attribute would parse, validate and be read by nobody.
type selectionRecorder struct {
	recordingWriter
	selections map[string]string
}

func (s *selectionRecorder) RegisterRoute(string, func(context.Context, map[string]interface{}) (interface{}, error)) {
}

func (s *selectionRecorder) SetOperationSelection(operation, selection string) {
	s.selections[operation] = selection
}

func TestTheRuntimeHandsEachFlowsSelectionToItsSource(t *testing.T) {
	src := &selectionRecorder{recordingWriter: recordingWriter{name: "products_api"}, selections: map[string]string{}}

	r := &Runtime{flows: NewFlowRegistry(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, cfg := range []*flow.Config{
		{Name: "prices", From: &flow.FromConfig{Connector: "products_api", ConnectorParams: map[string]interface{}{
			"operation": "Subscription.priceChanged", "selection": "{ sku newPrice }",
		}}},
		{Name: "created", From: &flow.FromConfig{Connector: "products_api", ConnectorParams: map[string]interface{}{
			"operation": "Subscription.productCreated",
		}}},
	} {
		r.flows.handlers[cfg.Name] = &FlowHandler{Config: cfg}
	}

	r.registerFlowHandlers("products_api", src)

	if got := src.selections["Subscription.priceChanged"]; got != "{ sku newPrice }" {
		t.Errorf("selection reaching the connector = %q, want %q", got, "{ sku newPrice }")
	}
	if _, set := src.selections["Subscription.productCreated"]; set {
		t.Error("a flow without a selection had one set")
	}
}

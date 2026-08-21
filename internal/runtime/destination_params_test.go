package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/transform"
)

// What a destination is told, beyond the payload.
//
// The reference documents a destination's params as CEL expressions and maps
// them to connector.Data.Params, and nothing ever put them there — a `to`
// block's params reached no connector at all. The S3 connector asks for the
// bytes in exactly that place, so a flow sending a message to a bucket was
// refused for want of content.
//
// And the target: a destination naming a field of the message was resolved
// against the payload, which a per-destination transform has already replaced
// by then — so a fan-out that shapes each destination wrote to a key called
// "input.filename".

func destinationHandler(t *testing.T, dest *flow.ToConfig, conn connector.Connector, withTransformer bool) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	registry.Replace("sink", conn)

	h := &FlowHandler{
		Config:     &flow.Config{Name: "send", MultiTo: []*flow.ToConfig{dest}},
		Connectors: registry,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if withTransformer {
		tr, err := transform.NewCELTransformer()
		if err != nil {
			t.Fatalf("NewCELTransformer: %v", err)
		}
		h.Transformer = tr
	}
	return h
}

func TestADestinationsParamsReachTheConnector(t *testing.T) {
	sink := &recordingWriter{name: "sink"}
	dest := &flow.ToConfig{
		Connector: "sink",
		ConnectorParams: map[string]interface{}{
			"target": "objects",
			"params": map[string]interface{}{
				"content": "input.body",
				"format":  "text",
			},
		},
	}

	h := destinationHandler(t, dest, sink, true)
	_, err := h.writeToDestination(context.Background(),
		map[string]interface{}{"body": "hello"},
		map[string]interface{}{"body": "hello"},
		dest, Operation{Method: "POST"})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	written := sink.writes()
	if len(written) != 1 {
		t.Fatalf("the connector was written to %d times", len(written))
	}
	if got := written[0].Params["content"]; got != "hello" {
		t.Errorf("params.content arrived as %#v; the destination said input.body", got)
	}
	if got := written[0].Params["format"]; got != "text" {
		t.Errorf("a constant param became %#v", got)
	}
}

func TestATargetNamingAFieldIsResolvedFromTheMessage(t *testing.T) {
	// The per-destination transform replaces the payload, so the target has to
	// be resolved against the message that arrived.
	sink := &recordingWriter{name: "sink"}
	dest := &flow.ToConfig{
		Connector: "sink",
		ConnectorParams: map[string]interface{}{
			"target": "input.filename",
		},
		Transform: map[string]string{"content": "input.body"},
	}

	h := destinationHandler(t, dest, sink, true)
	_, err := h.writeToDestination(context.Background(),
		map[string]interface{}{"filename": "report.txt", "body": "hello"},
		map[string]interface{}{"filename": "report.txt", "body": "hello"},
		dest, Operation{Method: "POST"})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	if got := sink.writes()[0].Target; got != "report.txt" {
		t.Errorf("wrote to %q, want the name the message carried", got)
	}
}

func TestShapingEachDestinationWithoutAFlowTransform(t *testing.T) {
	// A flow may shape each destination and declare no transform of its own,
	// and then nothing has built the evaluator. This took the service down
	// with a nil dereference on the first message.
	sink := &recordingWriter{name: "sink"}
	dest := &flow.ToConfig{
		Connector:       "sink",
		ConnectorParams: map[string]interface{}{"target": "objects"},
		Transform:       map[string]string{"who": "input.name"},
	}

	h := destinationHandler(t, dest, sink, false) // no transformer built

	_, err := h.writeToDestination(context.Background(),
		map[string]interface{}{"name": "Ada"},
		map[string]interface{}{"name": "Ada"},
		dest, Operation{Method: "POST"})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	payload, _ := sink.writes()[0].Payload["who"]
	if payload != "Ada" {
		t.Errorf("the destination's transform produced %#v", payload)
	}
}

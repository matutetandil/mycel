package flow

import (
	"encoding/json"
	"testing"
)

func TestWrapPayload(t *testing.T) {
	payload := map[string]interface{}{
		"style_number": "AI02LT",
		"websites":     map[string]interface{}{"us": true, "uk": false},
	}

	t.Run("empty key passes through", func(t *testing.T) {
		got := WrapPayload(payload, "")
		if got["style_number"] != "AI02LT" {
			t.Errorf("expected pass-through, got %+v", got)
		}
	})

	t.Run("non-empty key wraps", func(t *testing.T) {
		got := WrapPayload(payload, "productData")
		inner, ok := got["productData"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected wrapped map, got %T", got["productData"])
		}
		if inner["style_number"] != "AI02LT" {
			t.Errorf("inner payload wrong: %+v", inner)
		}
		if len(got) != 1 {
			t.Errorf("expected single root key, got %d", len(got))
		}
	})

	t.Run("nil payload still produces wrapper", func(t *testing.T) {
		got := WrapPayload(nil, "productData")
		if _, ok := got["productData"]; !ok {
			t.Errorf("expected productData key present, got %+v", got)
		}
		// Marshalling proves the wrapper is well-formed regardless of the
		// nil-vs-empty-map distinction the Go interface comparison hides.
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		if string(out) != `{"productData":null}` && string(out) != `{"productData":{}}` {
			t.Errorf("unexpected JSON: %s", out)
		}
	})

	t.Run("wrapped payload survives json round-trip", func(t *testing.T) {
		got := WrapPayload(payload, "productData")
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		var back map[string]interface{}
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		inner, _ := back["productData"].(map[string]interface{})
		if inner["style_number"] != "AI02LT" {
			t.Errorf("round-trip lost data: %+v", inner)
		}
	})
}

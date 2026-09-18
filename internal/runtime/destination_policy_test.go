package runtime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// What a destination says about the record it writes.
//
// conflict_key / on_conflict / ttl describe the record rather than its
// contents, and the point of them is that a flow can say "the last payload per
// SKU, kept for a month" without spelling out a filter document, an update
// document and an upsert param — three attributes that have to agree, in a
// block whose `params` are read on some write paths and not others.
//
// Which is the failure these guard: an attribute that parses, is stored, and
// reaches no connector. The destination has to receive them.

func policyHandler(t *testing.T, to *flow.ToConfig, conn connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	registry.Replace("store", conn)

	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	return &FlowHandler{
		Config: &flow.Config{
			Name: "archive_payload",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "POST /skus"},
			},
			To: to,
		},
		Connectors:  registry,
		Dest:        conn,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestADestinationsConflictPolicyReachesTheConnector(t *testing.T) {
	store := &recordingWriter{name: "store"}
	to := &flow.ToConfig{
		Connector: "store",
		ConnectorParams: map[string]interface{}{
			"target":       "payload_archive",
			"conflict_key": "sku",
			"on_conflict":  "replace",
			"ttl":          "30d",
		},
	}

	h := policyHandler(t, to, store)
	if _, err := h.HandleRequest(context.Background(), map[string]interface{}{
		"sku":  "ABC-1",
		"body": "whatever arrived",
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	written := store.writes()
	if len(written) != 1 {
		t.Fatalf("wrote %d times, want 1", len(written))
	}
	got := written[0]
	if len(got.ConflictKey) != 1 || got.ConflictKey[0] != "sku" {
		t.Errorf("conflict key = %v, want [sku]", got.ConflictKey)
	}
	if got.OnConflict != "replace" {
		t.Errorf("on_conflict = %q, want replace", got.OnConflict)
	}
	if got.TTL != 30*24*time.Hour {
		t.Errorf("ttl = %s, want 720h", got.TTL)
	}
}

// A composite key: `conflict_key = ["store", "sku"]`, because the same SKU can
// live in more than one store.
func TestAConflictKeyCanNameSeveralFields(t *testing.T) {
	store := &recordingWriter{name: "store"}
	to := &flow.ToConfig{
		Connector: "store",
		ConnectorParams: map[string]interface{}{
			"target":       "payload_archive",
			"conflict_key": []interface{}{"store", "sku"},
		},
	}

	h := policyHandler(t, to, store)
	if _, err := h.HandleRequest(context.Background(), map[string]interface{}{
		"store": "main",
		"sku":   "ABC-1",
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	got := store.writes()[0].ConflictKey
	if len(got) != 2 || got[0] != "store" || got[1] != "sku" {
		t.Errorf("conflict key = %v, want [store sku]", got)
	}
}

// A ttl nobody can parse is said out loud. Defaulting it to zero would mean
// "no expiry", which is the one failure nobody notices: everything keeps
// working and the store keeps growing.
func TestATTLThatCannotBeParsedIsRefused(t *testing.T) {
	store := &recordingWriter{name: "store"}
	to := &flow.ToConfig{
		Connector: "store",
		ConnectorParams: map[string]interface{}{
			"target": "payload_archive",
			"ttl":    "one month",
		},
	}

	h := policyHandler(t, to, store)
	_, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "ABC-1"})
	if err == nil {
		t.Fatal("a ttl of \"one month\" was accepted; it would have meant no expiry at all")
	}
	if !strings.Contains(err.Error(), "ttl") {
		t.Errorf("the error does not name the attribute: %v", err)
	}
	if len(store.writes()) != 0 {
		t.Error("the record was written anyway, without the expiry the flow asked for")
	}
}

// Saying nothing leaves the write as it was: no conflict key, no policy, no
// expiry.
func TestADestinationWithoutAPolicySaysNothingAboutTheRecord(t *testing.T) {
	store := &recordingWriter{name: "store"}
	to := &flow.ToConfig{
		Connector:       "store",
		ConnectorParams: map[string]interface{}{"target": "payload_archive"},
	}

	h := policyHandler(t, to, store)
	if _, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "ABC-1"}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	got := store.writes()[0]
	if len(got.ConflictKey) != 0 || got.OnConflict != "" || got.TTL != 0 {
		t.Errorf("an ordinary write carries a policy: key=%v on_conflict=%q ttl=%s",
			got.ConflictKey, got.OnConflict, got.TTL)
	}
}

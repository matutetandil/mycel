package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Writing the record that is already there, against a real MongoDB.
//
// What a flow wants to say is "this record, identified by these fields, and
// here is what to do if you already have it". Saying it in Mongo's own terms
// meant a filter document, an update document and an upsert param agreeing
// with each other, through a `params` attribute that some write paths read and
// others do not.
//
// The outcome is what these check, because the affected count cannot tell the
// cases apart: an upsert that inserted and one that overwrote a stored record
// both report one.

func archiveCollection(t *testing.T, c *Connector, name string) {
	t.Helper()
	ctx := context.Background()
	if err := c.db.Collection(name).Drop(ctx); err != nil {
		t.Fatalf("dropping %s: %v", name, err)
	}
	c.ttl.forget(name)
	t.Cleanup(func() {
		_ = c.db.Collection(name).Drop(context.Background())
	})
}

func documentsIn(t *testing.T, c *Connector, name string) []bson.M {
	t.Helper()
	cursor, err := c.db.Collection(name).Find(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var out []bson.M
	if err := cursor.All(context.Background(), &out); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return out
}

func TestTheLastPayloadPerKeyIsWhatIsKept(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "payload_archive")
	ctx := context.Background()

	write := func(sku, body string) *connector.Result {
		t.Helper()
		result, err := c.Write(ctx, &connector.Data{
			Target:      "payload_archive",
			Operation:   "INSERT",
			ConflictKey: []string{"sku"},
			OnConflict:  "replace",
			Payload:     map[string]interface{}{"sku": sku, "body": body},
		})
		if err != nil {
			t.Fatalf("writing %s: %v", sku, err)
		}
		return result
	}

	first := write("ABC-1", "first")
	if first.Metadata["outcome"] != "inserted" {
		t.Errorf("first write reported %v, want inserted", first.Metadata["outcome"])
	}

	second := write("ABC-1", "second")
	if second.Metadata["outcome"] != "replaced" {
		t.Errorf("second write reported %v, want replaced", second.Metadata["outcome"])
	}

	write("XYZ-9", "other")

	stored := documentsIn(t, c, "payload_archive")
	if len(stored) != 2 {
		t.Fatalf("the collection holds %d documents, want one per SKU: %v", len(stored), stored)
	}
	for _, doc := range stored {
		if doc["sku"] == "ABC-1" && doc["body"] != "second" {
			t.Errorf("ABC-1 holds %v, want the last payload", doc["body"])
		}
	}
}

// update merges, replace does not: a field the stored record has and the new
// payload does not survives an update and is gone after a replace. Which one
// somebody wants depends on whether their messages carry the whole record.
func TestUpdateMergesAndReplaceDoesNot(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "merge_archive")
	ctx := context.Background()

	if _, err := c.Write(ctx, &connector.Data{
		Target:      "merge_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "update",
		Payload:     map[string]interface{}{"sku": "ABC-1", "body": "first", "extra": "kept"},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if _, err := c.Write(ctx, &connector.Data{
		Target:      "merge_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "update",
		Payload:     map[string]interface{}{"sku": "ABC-1", "body": "second"},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	stored := documentsIn(t, c, "merge_archive")[0]
	if stored["body"] != "second" {
		t.Errorf("body = %v, want second", stored["body"])
	}
	if stored["extra"] != "kept" {
		t.Errorf("update dropped a field the payload did not mention: %v", stored)
	}

	if _, err := c.Write(ctx, &connector.Data{
		Target:      "merge_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "replace",
		Payload:     map[string]interface{}{"sku": "ABC-1", "body": "third"},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	stored = documentsIn(t, c, "merge_archive")[0]
	if _, found := stored["extra"]; found {
		t.Errorf("replace kept a field the payload did not mention: %v", stored)
	}
}

func TestSkipKeepsWhatIsStoredAndErrorRefuses(t *testing.T) {
	c := liveMongo(t)
	ctx := context.Background()

	archiveCollection(t, c, "skip_archive")
	for _, body := range []string{"first", "second"} {
		result, err := c.Write(ctx, &connector.Data{
			Target:      "skip_archive",
			ConflictKey: []string{"sku"},
			OnConflict:  "skip",
			Payload:     map[string]interface{}{"sku": "ABC-1", "body": body},
		})
		if err != nil {
			t.Fatalf("writing %s: %v", body, err)
		}
		if body == "second" && result.Metadata["outcome"] != "unchanged" {
			t.Errorf("the second write reported %v, want unchanged", result.Metadata["outcome"])
		}
	}
	stored := documentsIn(t, c, "skip_archive")
	if len(stored) != 1 || stored[0]["body"] != "first" {
		t.Errorf("skip did not keep the stored record: %v", stored)
	}

	archiveCollection(t, c, "error_archive")
	if _, err := c.Write(ctx, &connector.Data{
		Target:      "error_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "error",
		Payload:     map[string]interface{}{"sku": "ABC-1", "body": "first"},
	}); err != nil {
		t.Fatalf("the first write failed: %v", err)
	}

	_, err := c.Write(ctx, &connector.Data{
		Target:      "error_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "error",
		Payload:     map[string]interface{}{"sku": "ABC-1", "body": "second"},
	})
	if err == nil {
		t.Fatal("on_conflict = error accepted a second record with the same key")
	}
	if !strings.Contains(err.Error(), "already holds") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// A composite key: the same SKU in two stores is two records.
func TestAConflictKeyOfSeveralFields(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "composite_archive")
	ctx := context.Background()

	for _, store := range []string{"main", "outlet", "main"} {
		if _, err := c.Write(ctx, &connector.Data{
			Target:      "composite_archive",
			ConflictKey: []string{"store", "sku"},
			OnConflict:  "replace",
			Payload:     map[string]interface{}{"store": store, "sku": "ABC-1", "body": store},
		}); err != nil {
			t.Fatalf("writing %s: %v", store, err)
		}
	}

	stored := documentsIn(t, c, "composite_archive")
	if len(stored) != 2 {
		t.Errorf("the collection holds %d documents, want one per store/sku pair: %v", len(stored), stored)
	}
}

// The identity has to be in the payload. Without it every message would be
// written under a filter of {sku: null}, so the archive would hold one
// document — whichever message arrived last, under no key at all.
func TestAMissingConflictKeyFieldIsRefused(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "keyless_archive")

	_, err := c.Write(context.Background(), &connector.Data{
		Target:      "keyless_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "replace",
		Payload:     map[string]interface{}{"body": "no sku here"},
	})
	if err == nil {
		t.Fatal("a payload with no sku was written under a conflict key of sku")
	}
	if !strings.Contains(err.Error(), "sku") {
		t.Errorf("the error does not name the missing field: %v", err)
	}
}

func TestAnUnknownConflictPolicyIsRefused(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "unknown_policy")

	_, err := c.Write(context.Background(), &connector.Data{
		Target:      "unknown_policy",
		ConflictKey: []string{"sku"},
		OnConflict:  "ignore",
		Payload:     map[string]interface{}{"sku": "ABC-1"},
	})
	if err == nil {
		t.Fatal("on_conflict = ignore was accepted")
	}
	// The message lists what is accepted, since "ignore" and "skip" mean the
	// same thing to a person.
	if !strings.Contains(err.Error(), "skip") {
		t.Errorf("the error does not list the accepted values: %v", err)
	}
}

// Expiry is the store's job. Mycel creates the index and writes each record's
// own deadline; Mongo does the deleting, on its own schedule (up to a minute
// after the deadline), which is why this checks the index and the field rather
// than waiting for a document to disappear.
func TestATTLIsEnforcedByTheStore(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "expiring_archive")
	ctx := context.Background()

	before := time.Now().UTC()
	if _, err := c.Write(ctx, &connector.Data{
		Target:      "expiring_archive",
		ConflictKey: []string{"sku"},
		OnConflict:  "replace",
		TTL:         30 * 24 * time.Hour,
		Payload:     map[string]interface{}{"sku": "ABC-1", "body": "first"},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	stored := documentsIn(t, c, "expiring_archive")[0]
	deadline, ok := stored[expiresAt].(primitive.DateTime)
	if !ok {
		t.Fatalf("%s is %T, and a TTL index only reads a BSON date", expiresAt, stored[expiresAt])
	}
	at := deadline.Time()
	if at.Before(before.Add(29*24*time.Hour)) || at.After(before.Add(31*24*time.Hour)) {
		t.Errorf("the deadline is %s, about 30 days from %s expected", at, before)
	}

	// The index Mongo expires by, created with expireAfterSeconds: 0 so the
	// deadline in each document is what decides.
	cursor, err := c.db.Collection("expiring_archive").Indexes().List(ctx)
	if err != nil {
		t.Fatalf("listing indexes: %v", err)
	}
	var indexes []bson.M
	if err := cursor.All(ctx, &indexes); err != nil {
		t.Fatalf("decoding indexes: %v", err)
	}

	var found bool
	for _, index := range indexes {
		if index["name"] != "mycel_ttl" {
			continue
		}
		found = true
		seconds, ok := index["expireAfterSeconds"]
		if !ok {
			t.Error("the TTL index does not expire anything")
		}
		if asInt(seconds) != 0 {
			t.Errorf("expireAfterSeconds = %v, want 0 so each document's own deadline decides", seconds)
		}
	}
	if !found {
		t.Errorf("no TTL index on the collection, so nothing expires: %v", indexes)
	}
}

// A second write to the same collection must not ask the server for the index
// again, and must keep carrying a deadline.
//
// These writes name no operation, which is also the check that an unnamed one
// writes: every runtime path fills it in, so an empty one means a caller with
// nothing to derive it from, and the answer used to be "unsupported
// operation: " with nothing after the colon.
func TestEveryWriteCarriesItsOwnDeadline(t *testing.T) {
	c := liveMongo(t)
	archiveCollection(t, c, "deadline_archive")
	ctx := context.Background()

	for _, sku := range []string{"ABC-1", "ABC-2"} {
		if _, err := c.Write(ctx, &connector.Data{
			Target:  "deadline_archive",
			TTL:     time.Hour,
			Payload: map[string]interface{}{"sku": sku},
		}); err != nil {
			t.Fatalf("writing %s: %v", sku, err)
		}
	}

	for _, doc := range documentsIn(t, c, "deadline_archive") {
		if _, ok := doc[expiresAt]; !ok {
			t.Errorf("a document was written without a deadline: %v", doc)
		}
	}
}

func asInt(v interface{}) int64 {
	switch n := v.(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return -1
}

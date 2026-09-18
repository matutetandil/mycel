package mongodb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Writing the record that is already there, and writing one that should not
// stay forever.
//
// Both of these were reachable before, and only by spelling out the mechanics:
// an upsert meant a `query_filter` naming the key, an `update` document with
// its own `$set`, and `params = { upsert = true }` — three attributes that have
// to agree, in a block whose params are read on some write paths and not
// others. Expiry meant creating the index by hand in Mongo and knowing that a
// TTL index only reads BSON dates, so the `now()` a transform produces (a
// string) silently expired nothing.
//
// What a flow actually wants to say is which field identifies the record and
// what to do when it is already stored.

// expiresAt is the field carrying each document's own expiry instant. The TTL
// index is created with expireAfterSeconds: 0, so Mongo removes a document
// once the instant in this field has passed.
//
// Keeping the deadline in the document rather than in the index is what makes
// a `ttl` change take effect: expireAfterSeconds cannot be changed by
// createIndex (it needs collMod), so an index built from the configured
// duration would keep the duration it was first created with, and a flow that
// lowered its ttl from a month to a day would go on keeping documents for a
// month with nothing said.
const expiresAt = "_mycel_expires_at"

// conflictPolicies are the values on_conflict accepts.
var conflictPolicies = map[string]bool{
	"update":  true,
	"replace": true,
	"skip":    true,
	"error":   true,
}

// ConflictPolicies returns the accepted on_conflict values, for the schema and
// for error messages that list them.
func ConflictPolicies() []string {
	return []string{"update", "replace", "skip", "error"}
}

// ttlIndexes remembers which collections have had their TTL index ensured, so
// a write does not ask the server on every message.
type ttlIndexes struct {
	mu   sync.Mutex
	done map[string]bool
}

func (t *ttlIndexes) ensure(ctx context.Context, coll *mongo.Collection) error {
	t.mu.Lock()
	if t.done == nil {
		t.done = make(map[string]bool)
	}
	if t.done[coll.Name()] {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	// createIndex is idempotent for an identical index, so this is safe to
	// call again after a restart or a reload.
	model := mongo.IndexModel{
		Keys:    bson.D{{Key: expiresAt, Value: 1}},
		Options: options.Index().SetName("mycel_ttl").SetExpireAfterSeconds(0),
	}
	if _, err := coll.Indexes().CreateOne(ctx, model); err != nil {
		return fmt.Errorf("creating the TTL index on %s: %w", coll.Name(), err)
	}

	t.mu.Lock()
	t.done[coll.Name()] = true
	t.mu.Unlock()
	return nil
}

// forget drops what is remembered about a collection. Used by tests that
// recreate one.
func (t *ttlIndexes) forget(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.done, name)
}

// conflictWrite writes one document identified by its conflict key, resolving
// the case where the collection already holds it.
func (c *Connector) conflictWrite(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	policy := strings.ToLower(strings.TrimSpace(data.OnConflict))
	if policy == "" {
		policy = "update"
	}
	if !conflictPolicies[policy] {
		return nil, fmt.Errorf("on_conflict %q is not one of %s", data.OnConflict, strings.Join(ConflictPolicies(), ", "))
	}

	document := c.convertToBSON(data.Payload)
	if data.TTL > 0 {
		if err := c.ttl.ensure(ctx, coll); err != nil {
			return nil, err
		}
		// A BSON date, because that is the only thing a TTL index reads.
		document[expiresAt] = time.Now().Add(data.TTL).UTC()
	}

	filter := bson.M{}
	for _, field := range data.ConflictKey {
		value, found := document[field]
		if !found {
			// Writing a record whose identity is missing would quietly write
			// one document for every message under a filter of {field: null}.
			return nil, fmt.Errorf("conflict_key names %q and the payload has no such field, so the record cannot be identified", field)
		}
		filter[field] = value
	}

	switch policy {
	case "replace":
		result, err := coll.ReplaceOne(ctx, filter, document, options.Replace().SetUpsert(true))
		if err != nil {
			return nil, fmt.Errorf("replace failed: %w", err)
		}
		return conflictResult(result.MatchedCount, result.ModifiedCount, result.UpsertedCount, result.UpsertedID, "replaced"), nil

	case "update":
		result, err := coll.UpdateOne(ctx, filter, bson.M{"$set": document}, options.Update().SetUpsert(true))
		if err != nil {
			return nil, fmt.Errorf("update failed: %w", err)
		}
		return conflictResult(result.MatchedCount, result.ModifiedCount, result.UpsertedCount, result.UpsertedID, "updated"), nil

	case "skip":
		// $setOnInsert writes the document only when the upsert inserts. An
		// existing record is left exactly as it was stored.
		result, err := coll.UpdateOne(ctx, filter, bson.M{"$setOnInsert": document}, options.Update().SetUpsert(true))
		if err != nil {
			return nil, fmt.Errorf("insert failed: %w", err)
		}
		return conflictResult(result.MatchedCount, result.ModifiedCount, result.UpsertedCount, result.UpsertedID, "skipped"), nil

	case "error":
		// The unique index is what makes this a refusal by the store rather
		// than a read-then-write race between two consumers.
		if err := c.ensureUniqueIndex(ctx, coll, data.ConflictKey); err != nil {
			return nil, err
		}
		result, err := coll.InsertOne(ctx, document)
		if err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return nil, fmt.Errorf("%s already holds a record with %s: %w",
					coll.Name(), strings.Join(data.ConflictKey, ", "), err)
			}
			return nil, fmt.Errorf("insert failed: %w", err)
		}
		return &connector.Result{
			Affected: 1,
			LastID:   identifierOf(result.InsertedID),
			Metadata: map[string]interface{}{"outcome": "inserted"},
		}, nil
	}

	return nil, fmt.Errorf("on_conflict %q is not one of %s", policy, strings.Join(ConflictPolicies(), ", "))
}

// conflictResult reports what the store did, which is not derivable from the
// affected count alone: an upsert that inserted and one that changed a stored
// record both report one.
func conflictResult(matched, modified, upserted int64, upsertedID interface{}, action string) *connector.Result {
	outcome := action
	if upserted > 0 {
		outcome = "inserted"
	} else if modified == 0 && matched > 0 {
		// Matched and unchanged: either "skip" kept the stored record, or the
		// payload was identical to it.
		outcome = "unchanged"
	}

	result := &connector.Result{
		Affected: modified + upserted,
		Metadata: map[string]interface{}{
			"outcome":  outcome,
			"matched":  matched,
			"modified": modified,
			"upserted": upserted,
		},
	}
	if upsertedID != nil {
		result.LastID = identifierOf(upsertedID)
	}
	return result
}

// ensureUniqueIndex backs on_conflict = "error" with a constraint in the store.
func (c *Connector) ensureUniqueIndex(ctx context.Context, coll *mongo.Collection, fields []string) error {
	keys := bson.D{}
	for _, field := range fields {
		keys = append(keys, bson.E{Key: field, Value: 1})
	}
	model := mongo.IndexModel{
		Keys:    keys,
		Options: options.Index().SetName("mycel_conflict_key").SetUnique(true),
	}
	if _, err := coll.Indexes().CreateOne(ctx, model); err != nil {
		return fmt.Errorf("creating the unique index on %s(%s): %w", coll.Name(), strings.Join(fields, ", "), err)
	}
	return nil
}

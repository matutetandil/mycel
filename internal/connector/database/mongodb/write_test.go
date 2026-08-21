package mongodb

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// The write operations, against a real MongoDB.
//
// A driver is where a flow's intent becomes something a server does, and the
// operations that were never exercised are the ones that change more than one
// document at a time: an update_many with the wrong filter changes the whole
// collection, and nothing about the answer says so except the count.

func liveMongo(t *testing.T) *Connector {
	t.Helper()

	uri := os.Getenv("MYCEL_TEST_MONGO_URI")
	if uri == "" {
		if !reachable("127.0.0.1:37017") {
			t.Skip("no MongoDB reachable at 127.0.0.1:37017 (the integration stack publishes it)")
		}
		uri = "mongodb://mongo:mycel@127.0.0.1:37017/mycel_test?authSource=admin"
	}

	c := New("orders_store", uri, "mycel_test")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Skipf("MongoDB is not answering: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func reachable(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// collection gives each test its own, so one cannot see another's documents.
func collection(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test_%d", time.Now().UnixNano())
}

func TestSeveralDocumentsGoInAtOnce(t *testing.T) {
	// A flow writing an array of rows: one round trip rather than one per
	// document, which is the difference between a batch import finishing and
	// not.
	c := liveMongo(t)
	target := collection(t)
	ctx := context.Background()

	result, err := c.Write(ctx, &connector.Data{
		Target:    target,
		Operation: "INSERT_MANY",
		Params: map[string]interface{}{"documents": []interface{}{
			map[string]interface{}{"sku": "WIDGET-1", "on_hand": 10},
			map[string]interface{}{"sku": "WIDGET-2", "on_hand": 3},
		}},
	})
	if err != nil {
		t.Fatalf("INSERT_MANY: %v", err)
	}
	if result.Affected != 2 {
		t.Errorf("affected = %d, want both", result.Affected)
	}

	read, err := c.Read(ctx, connector.Query{Target: target})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(read.Rows) != 2 {
		t.Errorf("%d documents, want both", len(read.Rows))
	}
}

func TestAnUpdateChangesWhatItsFilterMatches(t *testing.T) {
	// The count is the only thing that says how much changed, so a flow that
	// meant one document and matched the collection finds out from here or
	// not at all.
	c := liveMongo(t)
	target := collection(t)
	ctx := context.Background()

	if _, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "INSERT_MANY",
		Params: map[string]interface{}{"documents": []interface{}{
			map[string]interface{}{"sku": "WIDGET-1", "status": "pending"},
			map[string]interface{}{"sku": "WIDGET-2", "status": "pending"},
			map[string]interface{}{"sku": "WIDGET-3", "status": "shipped"},
		}},
	}); err != nil {
		t.Fatalf("INSERT_MANY: %v", err)
	}

	// One document, named by its filter.
	result, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "UPDATE_ONE",
		Filters: map[string]interface{}{"sku": "WIDGET-1"},
		Payload: map[string]interface{}{"status": "paid"},
	})
	if err != nil {
		t.Fatalf("UPDATE_ONE: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d, want the one that matched", result.Affected)
	}

	// And every document the filter matches.
	result, err = c.Write(ctx, &connector.Data{
		Target: target, Operation: "UPDATE_MANY",
		Filters: map[string]interface{}{"status": "pending"},
		Payload: map[string]interface{}{"status": "cancelled"},
	})
	if err != nil {
		t.Fatalf("UPDATE_MANY: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d, want the one still pending", result.Affected)
	}

	// What was not matched is untouched, which is the half nobody checks.
	read, err := c.Read(ctx, connector.Query{
		Target: target, Filters: map[string]interface{}{"sku": "WIDGET-3"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(read.Rows) != 1 || read.Rows[0]["status"] != "shipped" {
		t.Errorf("a document the filter did not match was changed: %v", read.Rows)
	}
}

func TestReplacingADocumentLeavesOnlyWhatWasGiven(t *testing.T) {
	// Different from an update: the fields not written are gone, which is the
	// distinction somebody reaches for replace_one to get.
	c := liveMongo(t)
	target := collection(t)
	ctx := context.Background()

	if _, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "INSERT",
		Payload: map[string]interface{}{"sku": "WIDGET-1", "status": "pending", "note": "rush"},
	}); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "REPLACE_ONE",
		Filters: map[string]interface{}{"sku": "WIDGET-1"},
		Payload: map[string]interface{}{"sku": "WIDGET-1", "status": "paid"},
	})
	if err != nil {
		t.Fatalf("REPLACE_ONE: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d", result.Affected)
	}

	read, err := c.Read(ctx, connector.Query{Target: target})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(read.Rows) != 1 {
		t.Fatalf("%d documents", len(read.Rows))
	}
	if read.Rows[0]["status"] != "paid" {
		t.Errorf("status = %v", read.Rows[0]["status"])
	}
	if _, kept := read.Rows[0]["note"]; kept {
		t.Error("a field the replacement did not carry survived — that is an update, not a replace")
	}
}

func TestDeletingTakesWhatTheFilterMatchesAndNoMore(t *testing.T) {
	c := liveMongo(t)
	target := collection(t)
	ctx := context.Background()

	if _, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "INSERT_MANY",
		Params: map[string]interface{}{"documents": []interface{}{
			map[string]interface{}{"sku": "WIDGET-1", "status": "cancelled"},
			map[string]interface{}{"sku": "WIDGET-2", "status": "cancelled"},
			map[string]interface{}{"sku": "WIDGET-3", "status": "shipped"},
		}},
	}); err != nil {
		t.Fatalf("INSERT_MANY: %v", err)
	}

	result, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "DELETE_MANY",
		Filters: map[string]interface{}{"status": "cancelled"},
	})
	if err != nil {
		t.Fatalf("DELETE_MANY: %v", err)
	}
	if result.Affected != 2 {
		t.Errorf("affected = %d, want both cancelled ones", result.Affected)
	}

	read, err := c.Read(ctx, connector.Query{Target: target})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(read.Rows) != 1 || read.Rows[0]["sku"] != "WIDGET-3" {
		t.Errorf("what is left = %v, want the one that was not cancelled", read.Rows)
	}
}

func TestAnOperationNobodyImplementsIsRefused(t *testing.T) {
	// By name, rather than as a write that silently does nothing.
	c := liveMongo(t)
	ctx := context.Background()

	_, err := c.Write(ctx, &connector.Data{
		Target: collection(t), Operation: "UPSERT_EVERYTHING",
		Payload: map[string]interface{}{"sku": "WIDGET-1"},
	})
	if err == nil {
		t.Error("an operation nothing implements was accepted")
	}
}

func TestAggregatingAnswersWithWhatThePipelineProduced(t *testing.T) {
	// The reason to reach for MongoDB rather than a table: a flow asks for a
	// summary and gets it from the server.
	c := liveMongo(t)
	target := collection(t)
	ctx := context.Background()

	if _, err := c.Write(ctx, &connector.Data{
		Target: target, Operation: "INSERT_MANY",
		Params: map[string]interface{}{"documents": []interface{}{
			map[string]interface{}{"warehouse": "auckland", "on_hand": 10},
			map[string]interface{}{"warehouse": "auckland", "on_hand": 5},
			map[string]interface{}{"warehouse": "wellington", "on_hand": 3},
		}},
	}); err != nil {
		t.Fatalf("INSERT_MANY: %v", err)
	}

	rows, err := c.Aggregate(ctx, target, []bson.M{
		{"$group": bson.M{
			"_id":   "$warehouse",
			"total": bson.M{"$sum": "$on_hand"},
		}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d groups, want one per warehouse", len(rows))
	}

	totals := map[string]interface{}{}
	for _, row := range rows {
		totals[fmt.Sprint(row["_id"])] = row["total"]
	}
	if fmt.Sprint(totals["auckland"]) != "15" {
		t.Errorf("auckland = %v, want the sum the server computed", totals["auckland"])
	}
}

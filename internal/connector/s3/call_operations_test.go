package s3

import (
	"context"
	"strings"
	"testing"
)

// The operations a step can ask an object store for.
//
// The connector's page lists read and write first, and its Call — which is what
// a step reaches — handled neither: they existed only through the Reader and
// Writer interfaces, so they worked as a flow's source or destination and
// answered "unknown operation" from a step. The file connector answers both.

func TestAStepCanWriteAnObjectAndReadItBack(t *testing.T) {
	ctx := context.Background()
	c := liveBucket(t)

	const key = "note.txt"
	t.Cleanup(func() { _, _ = c.Call(ctx, "delete", map[string]interface{}{"key": key}) })

	if _, err := c.Call(ctx, "write", map[string]interface{}{
		"key":     key,
		"content": "hello from a step",
		"format":  "text",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	read, err := c.Call(ctx, "read", map[string]interface{}{"key": key, "format": "text"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	row, ok := read.(map[string]interface{})
	if !ok {
		t.Fatalf("read answered %#v", read)
	}
	if content, _ := row["content"].(string); !strings.Contains(content, "hello from a step") {
		t.Errorf("what came back was %#v", row)
	}
}

func TestAnOperationNobodyImplementsSaysSo(t *testing.T) {
	ctx := context.Background()
	c := liveBucket(t)

	_, err := c.Call(ctx, "teleport", map[string]interface{}{"key": "x"})
	if err == nil {
		t.Fatal("an operation that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "teleport") {
		t.Errorf("the error does not name the operation: %v", err)
	}
}

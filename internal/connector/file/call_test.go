package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// The operations a step can ask a file connector for.
//
// Call is the door a `step` or an `enrich` goes through, and half of what it
// offers had never been called: delete, exists, stat, list, copy and move. An
// operation that is offered and does not work is worse than one that is
// missing, because the configuration that uses it looks right.
func TestEveryOperationAStepCanAskFor(t *testing.T) {
	dir := t.TempDir()
	c := New("files", &Config{BasePath: dir, Format: "text"})
	ctx := context.Background()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })
	if err := c.Health(ctx); err != nil {
		t.Errorf("health: %v", err)
	}

	// Written.
	if _, err := c.Call(ctx, "write", map[string]interface{}{
		"path": "notes.txt", "content": "first\n", "format": "text",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	// There.
	exists, err := c.Call(ctx, "exists", map[string]interface{}{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !says(exists, "exists", true) {
		t.Errorf("exists = %#v, want it to say the file is there", exists)
	}

	missing, err := c.Call(ctx, "exists", map[string]interface{}{"path": "nothing.txt"})
	if err != nil {
		t.Fatalf("exists on a missing file: %v", err)
	}
	if !says(missing, "exists", false) {
		t.Errorf("exists on a missing file = %#v", missing)
	}

	// Described.
	stat, err := c.Call(ctx, "stat", map[string]interface{}{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !hasKeys(stat, "size", "name") {
		t.Errorf("stat = %#v, want at least a size and a name", stat)
	}

	// Read back.
	read, err := c.Call(ctx, "read", map[string]interface{}{"path": "notes.txt", "format": "text"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(flatten(read), "first") {
		t.Errorf("read = %#v", read)
	}

	// Copied, then moved.
	if _, err := c.Call(ctx, "copy", map[string]interface{}{
		"source": "notes.txt", "destination": "copy.txt",
	}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "copy.txt")); err != nil {
		t.Errorf("the copy is not there: %v", err)
	}

	if _, err := c.Call(ctx, "move", map[string]interface{}{
		"source": "copy.txt", "destination": "moved.txt",
	}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "copy.txt")); err == nil {
		t.Error("the file is still at its old name after a move")
	}
	if _, err := os.Stat(filepath.Join(dir, "moved.txt")); err != nil {
		t.Errorf("the moved file is not at its new name: %v", err)
	}

	// Listed.
	listing, err := c.Call(ctx, "list", map[string]interface{}{"path": "."})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(flatten(listing), "notes.txt") {
		t.Errorf("the listing does not have the file: %#v", listing)
	}

	// Gone.
	if _, err := c.Call(ctx, "delete", map[string]interface{}{"path": "moved.txt"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "moved.txt")); err == nil {
		t.Error("the file is still there after being deleted")
	}

	// And an operation nobody has is refused by name.
	if _, err := c.Call(ctx, "sharpen", map[string]interface{}{}); err == nil {
		t.Error("an operation this connector does not have was accepted")
	} else if !strings.Contains(err.Error(), "sharpen") {
		t.Errorf("the refusal reads %q; it should name what was asked for", err)
	}
}

// Text read as lines, which is what a flow that processes a file row by row
// asks for.
func TestTextComesBackWholeOrAsLines(t *testing.T) {
	dir := t.TempDir()
	c := New("files", &Config{BasePath: dir, Format: "text"})
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("one\ntwo\nthree"), 0o644); err != nil {
		t.Fatal(err)
	}

	whole, err := c.Read(ctx, connector.Query{
		Target: "lines.txt",
		Params: map[string]interface{}{"format": "text"},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(whole.Rows) != 1 || !strings.Contains(whole.Rows[0]["content"].(string), "two") {
		t.Errorf("whole = %#v", whole.Rows)
	}

	// Line by line is a format of its own rather than an option on text.
	byLine, err := c.Read(ctx, connector.Query{
		Target: "lines.txt",
		Params: map[string]interface{}{"format": "lines"},
	})
	if err != nil {
		t.Fatalf("read by lines: %v", err)
	}
	if len(byLine.Rows) != 3 {
		t.Fatalf("%d rows, want one per line", len(byLine.Rows))
	}
	if byLine.Rows[0]["line"] != 1 || byLine.Rows[0]["content"] != "one" {
		t.Errorf("first row = %#v", byLine.Rows[0])
	}
}

// Bytes go out and come back as bytes.
func TestBinaryGoesThroughUnchanged(t *testing.T) {
	dir := t.TempDir()
	c := New("files", &Config{BasePath: dir, Format: "binary"})
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	content := []byte{0x00, 0x01, 0xff, 0xfe, 0x7f}
	if _, err := c.Write(ctx, &connector.Data{
		Target:  "blob.bin",
		Payload: map[string]interface{}{"data": content},
		Params:  map[string]interface{}{"format": "binary"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	read, err := c.Read(ctx, connector.Query{
		Target: "blob.bin",
		Params: map[string]interface{}{"format": "binary"},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(read.Rows) != 1 {
		t.Fatalf("%d rows", len(read.Rows))
	}
	got, ok := read.Rows[0]["data"].([]byte)
	if !ok {
		t.Fatalf("data = %#v (%T), want bytes", read.Rows[0]["data"], read.Rows[0]["data"])
	}
	if string(got) != string(content) {
		t.Errorf("the bytes came back changed: %#v", got)
	}
	if size, ok := read.Rows[0]["size"].(int); !ok || size != len(content) {
		t.Errorf("size = %#v, want %d", read.Rows[0]["size"], len(content))
	}
}

func says(answer interface{}, key string, want bool) bool {
	row, ok := answer.(map[string]interface{})
	if !ok {
		if rows, isList := answer.([]map[string]interface{}); isList && len(rows) > 0 {
			row = rows[0]
		} else {
			return false
		}
	}
	got, present := row[key]
	return present && got == want
}

func hasKeys(answer interface{}, keys ...string) bool {
	row, ok := answer.(map[string]interface{})
	if !ok {
		if rows, isList := answer.([]map[string]interface{}); isList && len(rows) > 0 {
			row = rows[0]
		} else {
			return false
		}
	}
	for _, key := range keys {
		if _, present := row[key]; !present {
			return false
		}
	}
	return true
}

func flatten(answer interface{}) string {
	return strings.ToLower(strings.TrimSpace(sprint(answer)))
}

func sprint(v interface{}) string {
	return fmt.Sprintf("%v", v)
}

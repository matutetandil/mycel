package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A flow may write to a file. The documentation shows one, and the connector
// could not be a destination at all: its Read and Write did not have the shapes
// connector.Reader and connector.Writer require, so a flow naming it was
// answered "destination connector does not support required operation". The
// files example had its upload and download flows commented out with a note
// blaming the parser for it.

func fileConnector(t *testing.T) (*Connector, string) {
	t.Helper()
	dir := t.TempDir()
	c := New("files", &Config{
		BasePath:   dir,
		Format:     "json",
		CreateDirs: true,
	})
	return c, dir
}

func TestAFlowCanWriteToAFile(t *testing.T) {
	c, dir := fileConnector(t)

	var _ connector.Writer = c

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "reports/latest.json",
		Payload: map[string]interface{}{"total": 5},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d, want 1", result.Affected)
	}

	written, err := os.ReadFile(filepath.Join(dir, "reports", "latest.json"))
	if err != nil {
		t.Fatalf("nothing was written: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(written, &got); err != nil {
		t.Fatalf("what was written is not the payload: %v", err)
	}
	if got["total"] != float64(5) {
		t.Errorf("file holds %v", got)
	}
}

func TestAFlowCanReadFromAFile(t *testing.T) {
	c, dir := fileConnector(t)

	var _ connector.Reader = c

	if err := os.WriteFile(filepath.Join(dir, "users.json"),
		[]byte(`[{"name":"Alice"},{"name":"Bob"}]`), 0o644); err != nil {
		t.Fatalf("preparing the file: %v", err)
	}

	result, err := c.Read(context.Background(), connector.Query{Target: "users.json"})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(result.Rows) != 2 || result.Rows[0]["name"] != "Alice" {
		t.Errorf("rows = %v", result.Rows)
	}
}

func TestTheFileWrittenIsTheOneTheMessageNames(t *testing.T) {
	// An upload names its own file, and a destination writes that the way
	// everything else refers to a field of the message.
	c, dir := fileConnector(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Target:  "input.filename",
		Payload: map[string]interface{}{"filename": "invoice.json", "total": 1},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "invoice.json")); err != nil {
		t.Errorf("the file the message named was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "input.filename")); err == nil {
		t.Error("a file called input.filename was written; the target was taken literally")
	}
}

func TestAFilenameFromOutsideCannotEscapeTheBasePath(t *testing.T) {
	// The target is resolved from the message, so the name comes from whoever
	// is calling. base_path still confines it.
	c, dir := fileConnector(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Target:  "input.filename",
		Payload: map[string]interface{}{"filename": "../escaped.json", "total": 1},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	outside := filepath.Join(filepath.Dir(dir), "escaped.json")
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("a file was written outside base_path at %s", outside)
	}
}

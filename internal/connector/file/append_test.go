package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Appending, declared once on the connector.
//
// A single write could already ask for it through its params, but nothing
// reaches those params from a flow's `to` block or from an aspect's action —
// which is exactly where a request log is written from. So the connector that
// was supposed to keep a log replaced it on every message: three requests in,
// the file held the third one only.
//
// And the shape matters as much as the mode. Appending indented JSON documents
// end to end produces a file no parser accepts, so an appended JSON write is
// JSONL: one compact object per line.

func appendingConnector(t *testing.T, appending bool) (*Connector, string) {
	t.Helper()
	dir := t.TempDir()
	return New("logs", &Config{
		BasePath:   dir,
		CreateDirs: true,
		Append:     appending,
	}), dir
}

func TestTheConnectorsAppendSettingKeepsEveryWrite(t *testing.T) {
	conn, dir := appendingConnector(t, true)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		if _, err := conn.Write(ctx, &connector.Data{
			Target:  "requests.log",
			Payload: map[string]interface{}{"name": name},
		}); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	content, err := os.ReadFile(filepath.Join(dir, "requests.log"))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 3 {
		t.Fatalf("the log holds %d records, want 3 — writes replaced each other:\n%s", len(lines), content)
	}

	// Every line stands on its own, which is the point of JSONL.
	want := []string{"first", "second", "third"}
	for i, line := range lines {
		var record map[string]interface{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d is not a JSON document on its own (%v): %s", i+1, err, line)
		}
		if record["name"] != want[i] {
			t.Errorf("line %d holds %v, want %s", i+1, record["name"], want[i])
		}
	}
}

// Without the setting nothing changes: a write still replaces the file, and
// JSON is still the indented document it always was.
func TestWithoutAppendAWriteStillReplacesTheFile(t *testing.T) {
	conn, dir := appendingConnector(t, false)
	ctx := context.Background()

	for _, name := range []string{"first", "second"} {
		if _, err := conn.Write(ctx, &connector.Data{
			Target:  "state.json",
			Payload: map[string]interface{}{"name": name},
		}); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	content, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	var record map[string]interface{}
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatalf("the file is not a JSON document: %v\n%s", err, content)
	}
	if record["name"] != "second" {
		t.Errorf("file holds %v, want second", record["name"])
	}
	if !strings.Contains(string(content), "\n  ") {
		t.Error("a replacing write no longer produces an indented document")
	}
}

// A single write still overrides the connector, in both directions: that is
// how a connector that appends by default can write a whole file once.
func TestASingleWriteOverridesTheConnectorsAppendSetting(t *testing.T) {
	ctx := context.Background()

	conn, dir := appendingConnector(t, true)
	for range 2 {
		if _, err := conn.Write(ctx, &connector.Data{
			Target:  "snapshot.json",
			Payload: map[string]interface{}{"name": "only"},
			Params:  map[string]interface{}{"append": false},
		}); err != nil {
			t.Fatalf("writing: %v", err)
		}
	}
	content, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if strings.Count(string(content), "only") != 1 {
		t.Errorf("append = false on the write did not replace the file:\n%s", content)
	}

	plain, dir := appendingConnector(t, false)
	for range 2 {
		if _, err := plain.Write(ctx, &connector.Data{
			Target:  "audit.log",
			Payload: map[string]interface{}{"name": "kept"},
			Params:  map[string]interface{}{"append": true},
		}); err != nil {
			t.Fatalf("writing: %v", err)
		}
	}
	content, err = os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if strings.Count(string(content), "kept") != 2 {
		t.Errorf("append = true on the write did not append:\n%s", content)
	}
}

// Text and CSV are already per-record shapes, so appending must not rewrite
// them into anything: a text log keeps the exact bytes it was given.
func TestAppendingLeavesTextAlone(t *testing.T) {
	conn, dir := appendingConnector(t, true)
	ctx := context.Background()

	for _, line := range []string{"one\n", "two\n"} {
		if _, err := conn.Write(ctx, &connector.Data{
			Target: "plain.log",
			Params: map[string]interface{}{"content": line, "format": "text"},
		}); err != nil {
			t.Fatalf("writing: %v", err)
		}
	}

	content, err := os.ReadFile(filepath.Join(dir, "plain.log"))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(content) != "one\ntwo\n" {
		t.Errorf("text log = %q, want %q", content, "one\ntwo\n")
	}
}

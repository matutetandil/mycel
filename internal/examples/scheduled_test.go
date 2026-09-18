package examples

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The scheduled example, waited on until its clock actually fires.
//
// Every other example is exercised by running the commands its README shows.
// This one has nothing to call: its flows are triggered by a cron expression,
// so the only way to know it works is to wait for a tick. Nothing ever did,
// and two separate faults lived there for three months — the first tick took
// the whole service down with a nil dereference (a scheduled flow has no
// `from` block, and the tracing span read its connector as a field), and once
// that was fixed the flow that says it inserts a heartbeat ran
// `SELECT * FROM heartbeats` instead, wrote nothing, and reported success.
//
// The published schedules are @every 5m and daily, which is why watching the
// service for a few seconds never showed either. This runs the example as
// written and only shortens the interval.

var everyFiveMinutes = regexp.MustCompile(`@every\s+5m`)

func TestTheScheduledExampleFiresOnItsOwn(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	// The example as published, with the one interval shortened so a tick
	// arrives while the test is still running. Nothing else is touched.
	source := t.TempDir()
	if err := copyTree(repoPath("examples", "scheduled"), source); err != nil {
		t.Fatalf("copying the example: %v", err)
	}
	shortened := 0
	_ = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".mycel") {
			return nil
		}
		text, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if !everyFiveMinutes.Match(text) {
			return nil
		}
		shortened++
		return os.WriteFile(path, everyFiveMinutes.ReplaceAll(text, []byte("@every 1s")), 0o644)
	})
	if shortened == 0 {
		t.Fatal("the scheduled example no longer has an @every 5m flow — this test is watching for a tick that will never come")
	}

	svc := startDir(t, source, "scheduled")

	// The heartbeat flow inserts one row per tick.
	db := filepath.Join(svc.dir, "data", "app.db")
	deadline := time.Now().Add(20 * time.Second)
	var rows int
	for time.Now().Before(deadline) {
		rows = countRows(t, db, "heartbeats")
		if rows > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	log := svc.tail()
	if strings.Contains(log, "panic:") {
		t.Fatalf("the first scheduled tick crashed the service:\n%s", log)
	}
	if rows == 0 {
		t.Fatalf("the scheduled flow ran and wrote no heartbeat — a flow triggered by the clock was treated as a read.\n%s", log)
	}
}

func copyTree(from, to string) error {
	return filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, info.Mode())
	})
}

func countRows(t *testing.T, database, table string) int {
	t.Helper()
	if _, err := os.Stat(database); err != nil {
		return 0
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return 0
	}
	defer db.Close()

	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		return 0
	}
	return n
}

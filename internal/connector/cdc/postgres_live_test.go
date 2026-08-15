package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Watching a real database.
//
// Everything below the decoding was untested: connecting for replication,
// creating the publication and the slot, and the loop that reads the stream.
// None of it can be stood up in a unit test — logical replication is a mode of
// the server, not a library — so it runs against the Postgres in the
// integration stack, where wal_level is already logical.
//
// What it proves is the thing that decoding cannot: that a row written by
// somebody else arrives here at all, in order, and that a slot left behind is
// a slot Postgres keeps WAL for until the disk fills.

func livePostgres(t *testing.T) (*Config, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYCEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set MYCEL_TEST_POSTGRES_DSN to run this against a real database")
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("MYCEL_TEST_POSTGRES_DSN: %v", err)
	}
	port, _ := strconv.Atoi(parsed.Port())
	if port == 0 {
		port = 5432
	}
	password, _ := parsed.User.Password()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("no database at %s: %v", parsed.Host, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &Config{
		Driver:   "postgres",
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		User:     parsed.User.Username(),
		Password: password,
		SSLMode:  "disable",
	}, db
}

func TestChangesToARealTableArriveAsEvents(t *testing.T) {
	config, db := livePostgres(t)

	// A slot and a publication of their own, so a run does not disturb one
	// already there and does not leave the next run reading old changes.
	suffix := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	config.SlotName = "mycel_live_" + suffix
	config.Publication = "mycel_live_pub_" + suffix
	table := "cdc_live_" + suffix

	ctx := context.Background()
	mustExec(t, db, fmt.Sprintf(
		`CREATE TABLE %s (id int primary key, name text, amount numeric, active boolean)`, table))
	// Without this, an update or a delete carries only the key, and a flow
	// reading input.old sees nothing but the id.
	mustExec(t, db, fmt.Sprintf(`ALTER TABLE %s REPLICA IDENTITY FULL`, table))

	t.Cleanup(func() {
		_, _ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, table))
		_, _ = db.Exec(fmt.Sprintf(`DROP PUBLICATION IF EXISTS %s`, config.Publication))
		// The slot is the one that matters: Postgres keeps every WAL segment a
		// slot has not confirmed, so one left behind fills the disk.
		dropSlot(db, config.SlotName)
	})

	listener := NewPostgresListener(config, nil)
	events := make(chan *Event, 16)
	done := make(chan error, 1)

	streamCtx, stopStreaming := context.WithCancel(ctx)
	defer stopStreaming()
	go func() { done <- listener.Start(streamCtx, events) }()

	// The slot starts where the stream started, so anything written before it
	// is connected is not in it. Wait for the slot to exist first.
	waitForSlot(t, db, config.SlotName)

	mustExec(t, db, fmt.Sprintf(
		`INSERT INTO %s (id, name, amount, active) VALUES (1, 'first order', 42.50, true)`, table))
	mustExec(t, db, fmt.Sprintf(`UPDATE %s SET name = 'renamed' WHERE id = 1`, table))
	mustExec(t, db, fmt.Sprintf(`DELETE FROM %s WHERE id = 1`, table))

	insert := waitForEvent(t, events, table)
	if insert.Trigger != "INSERT" {
		t.Fatalf("first event = %s, want INSERT", insert.Trigger)
	}
	if insert.Schema != "public" || insert.Table != table {
		t.Errorf("event names %s.%s", insert.Schema, insert.Table)
	}
	// The column types come from the relation message the server sent, not
	// from anything configured — a number arriving as text is what a flow
	// doing arithmetic on it trips over.
	if insert.New["name"] != "first order" {
		t.Errorf("name = %v", insert.New["name"])
	}
	if amount, ok := insert.New["amount"].(float64); !ok || amount != 42.50 {
		t.Errorf("amount = %#v, want a number", insert.New["amount"])
	}
	if active, ok := insert.New["active"].(bool); !ok || !active {
		t.Errorf("active = %#v, want a boolean", insert.New["active"])
	}
	if insert.Old != nil {
		t.Errorf("an insert carried a previous row: %v", insert.Old)
	}

	update := waitForEvent(t, events, table)
	if update.Trigger != "UPDATE" {
		t.Fatalf("second event = %s, want UPDATE", update.Trigger)
	}
	if update.New["name"] != "renamed" {
		t.Errorf("new name = %v", update.New["name"])
	}
	// This is what REPLICA IDENTITY FULL buys, and the reason a flow can tell
	// what changed rather than only what it now is.
	if update.Old == nil || update.Old["name"] != "first order" {
		t.Errorf("previous row = %v, want the name before the update", update.Old)
	}

	deleted := waitForEvent(t, events, table)
	if deleted.Trigger != "DELETE" {
		t.Fatalf("third event = %s, want DELETE", deleted.Trigger)
	}
	if deleted.Old == nil || deleted.Old["name"] != "renamed" {
		t.Errorf("deleted row = %v", deleted.Old)
	}
	if deleted.New != nil {
		t.Errorf("a delete carried a new row: %v", deleted.New)
	}

	// Shutting down has to give the connection back. A replication connection
	// left open holds its slot, and the next run cannot use it.
	stopStreaming()
	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("streaming ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("streaming did not stop when the context was cancelled")
	}
	if err := listener.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Closing twice is what shutdown does when the supervisor has already
	// closed it, and it must not be an error.
	if err := listener.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAStreamPicksUpWhereItLeftOff(t *testing.T) {
	// The point of a slot: a consumer that was down does not lose the changes
	// written while it was, which is the whole reason CDC is not polling.
	config, db := livePostgres(t)

	suffix := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	config.SlotName = "mycel_resume_" + suffix
	config.Publication = "mycel_resume_pub_" + suffix
	table := "cdc_resume_" + suffix

	mustExec(t, db, fmt.Sprintf(`CREATE TABLE %s (id int primary key, name text)`, table))
	t.Cleanup(func() {
		_, _ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, table))
		_, _ = db.Exec(fmt.Sprintf(`DROP PUBLICATION IF EXISTS %s`, config.Publication))
		dropSlot(db, config.SlotName)
	})

	first := NewPostgresListener(config, nil)
	events := make(chan *Event, 16)
	firstCtx, stopFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Start(firstCtx, events) }()

	waitForSlot(t, db, config.SlotName)
	mustExec(t, db, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'before')`, table))
	if e := waitForEvent(t, events, table); e.New["name"] != "before" {
		t.Fatalf("first event = %v", e.New)
	}

	stopFirst()
	<-firstDone
	_ = first.Close()

	// Written while nobody was listening.
	mustExec(t, db, fmt.Sprintf(`INSERT INTO %s VALUES (2, 'while away')`, table))

	second := NewPostgresListener(config, nil)
	secondCtx, stopSecond := context.WithCancel(context.Background())
	defer stopSecond()
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Start(secondCtx, events) }()

	// The change written during the gap has to arrive. What may come before it
	// is the earlier one again: how far a consumer got is only reported to the
	// server periodically, so anything not yet confirmed is sent a second time.
	// At-least-once is the contract, and a flow that must not act twice says so
	// with a dedupe block — but nothing may be missing.
	var seen []string
	for {
		e := waitForEvent(t, events, table)
		seen = append(seen, fmt.Sprint(e.New["name"]))
		if e.New["name"] == "while away" {
			break
		}
		if len(seen) > 4 {
			t.Fatalf("the change written while the stream was down never arrived; got %v", seen)
		}
	}

	stopSecond()
	<-secondDone
	_ = second.Close()
}

func mustExec(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

// waitForSlot waits until the listener has created its replication slot, which
// is the point from which changes are captured.
func waitForSlot(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var found int
		err := db.QueryRow(`SELECT count(*) FROM pg_replication_slots WHERE slot_name = $1`, name).Scan(&found)
		if err == nil && found == 1 {
			// The slot exists a moment before the stream is reading; give the
			// server that moment rather than racing it.
			time.Sleep(300 * time.Millisecond)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("replication slot %s was never created", name)
}

// waitForEvent returns the next change to the table under test, ignoring
// anything the rest of the stack happens to be writing at the same time.
func waitForEvent(t *testing.T, events <-chan *Event, table string) *Event {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Table == table {
				return e
			}
		case <-deadline:
			t.Fatalf("no change to %s arrived", table)
		}
	}
}

// dropSlot releases the slot, retrying while the server still considers the
// replication connection active.
func dropSlot(db *sql.DB, name string) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := db.Exec(`SELECT pg_drop_replication_slot($1)`, name); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

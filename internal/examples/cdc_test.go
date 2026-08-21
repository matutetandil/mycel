package examples

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// The CDC example, watching a real database.
//
// Its README shows no commands to run: the example is a source, and what it
// does is react to somebody else writing. So this writes the row and looks for
// what the flow was supposed to make of it — which is the only way to know the
// example works, and nothing had ever done it.

func TestTheCDCExampleTurnsAChangeIntoAnEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	address := os.Getenv("MYCEL_TEST_POSTGRES_DSN")
	if address == "" {
		t.Skip("set MYCEL_TEST_POSTGRES_DSN to run this against a real database")
	}

	// A database of this example's own: the shared one already has tables of
	// these names, with columns from whoever made them.
	own := freshPostgres(t, "cdc")
	if own == nil {
		t.Skip("no Postgres to watch")
	}

	source, err := sql.Open("pgx", own.String())
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer source.Close()
	if err := source.Ping(); err != nil {
		t.Skipf("nothing answers at %s: %v", own.Host, err)
	}

	// A slot and a publication of this run's own, so it neither disturbs
	// another nor reads what an earlier one left behind.
	suffix := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	slot := "cdc_example_" + suffix
	publication := "cdc_example_pub_" + suffix

	mustRun(t, source, `CREATE TABLE IF NOT EXISTS users (id serial primary key, email text)`)
	t.Cleanup(func() {
		_, _ = source.Exec(`DROP PUBLICATION IF EXISTS ` + publication)
		// The slot is the one that matters: Postgres keeps every WAL segment a
		// slot has not confirmed, so one left behind fills the disk.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := source.Exec(`SELECT pg_drop_replication_slot($1)`, slot); err == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	})

	password, _ := own.User.Password()
	parsed := own
	svc := start(t, "cdc",
		"DB_HOST="+parsed.Hostname(),
		"DB_PORT="+parsed.Port(),
		"DB_NAME="+strings.TrimPrefix(parsed.Path, "/"),
		"DB_USER="+parsed.User.Username(),
		"DB_PASSWORD="+password,
		"CDC_SLOT="+slot,
		"CDC_PUBLICATION="+publication,
	)

	// The slot exists a moment after the service does; anything written before
	// it is connected is not in it.
	waitForSlot(t, source, slot)

	mustRun(t, source, `INSERT INTO users (email) VALUES ('watched@example.com')`)

	events := filepath.Join(svc.dir, "data", "events.db")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if row := eventFor(t, events, "watched@example.com"); row != "" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Errorf("a row written to the watched table never became an event; the service said:\n%s", svc.tail())
}

func mustRun(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func waitForSlot(t *testing.T, db *sql.DB, slot string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var found int
		if err := db.QueryRow(
			`SELECT count(*) FROM pg_replication_slots WHERE slot_name = $1`, slot).Scan(&found); err == nil && found == 1 {
			time.Sleep(500 * time.Millisecond)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the example never created its replication slot %s", slot)
}

// eventFor reports the event the flow wrote for this address, if it is there
// yet. The database may not exist until the service has written to it.
func eventFor(t *testing.T, path, email string) string {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return ""
	}
	defer db.Close()

	var event string
	if err := db.QueryRow(`SELECT event FROM events WHERE email = ?`, email).Scan(&event); err != nil {
		return ""
	}
	return event
}

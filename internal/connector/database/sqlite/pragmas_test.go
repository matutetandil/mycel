package sqlite

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/database"
)

func TestDSNCarriesThePragmas(t *testing.T) {
	dsn := database.SQLiteDSN

	for name, tc := range map[string]struct {
		path     string
		wants    []string
		notWants []string
	}{
		"a plain path": {
			path:  "./data/app.db",
			wants: []string{"file:./data/app.db?", "busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"},
		},
		"an in-memory database": {
			path:  ":memory:",
			wants: []string{"busy_timeout(5000)", "foreign_keys(1)"},
			// WAL is a file next to the database, so it means nothing here.
			notWants: []string{"journal_mode"},
		},
		"a file URI that already has parameters": {
			path:  "file:app.db?mode=rwc",
			wants: []string{"file:app.db?mode=rwc&", "busy_timeout(5000)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := dsn(tc.path)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("dsn(%q) = %q, missing %q", tc.path, got, want)
				}
			}
			for _, unwanted := range tc.notWants {
				if strings.Contains(got, unwanted) {
					t.Errorf("dsn(%q) = %q, should not contain %q", tc.path, got, unwanted)
				}
			}
		})
	}
}

// Concurrent writers have to queue, not fail.
//
// The connector opened SQLite with the defaults: no busy timeout and the
// rollback journal. A write that found the database locked gave up at once
// with SQLITE_BUSY rather than waiting its turn, so a service backed by SQLite
// — which is what the quick start and most of the examples use — failed the
// majority of its requests as soon as two of them overlapped. Measured against
// the old code under ten concurrent writers: 63% of requests failed.
func TestConcurrentWritersDoNotCollide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	conn := New("test", path, slog.Default())

	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.db.ExecContext(ctx, `CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const writers, each = 10, 20
	errs := make(chan error, writers*each)
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				_, err := conn.Write(ctx, &connector.Data{
					Target:    "items",
					Operation: "INSERT",
					Payload: map[string]interface{}{
						"id":   fmt.Sprintf("%d-%d", w, i),
						"name": "item",
					},
				})
				if err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	failed := 0
	var first error
	for err := range errs {
		if first == nil {
			first = err
		}
		failed++
	}
	if failed > 0 {
		t.Errorf("%d of %d concurrent writes failed, first: %v", failed, writers*each, first)
	}

	var count int
	if err := conn.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != writers*each {
		t.Errorf("%d rows written, want %d", count, writers*each)
	}
}

// The foreign_keys pragma has to hold on every connection the pool opens.
//
// It used to be one PRAGMA executed after Open, which SQLite applies to
// whichever pooled connection served it. Every other connection the pool
// opened later — that is, every connection under load — had foreign keys off,
// so the same violating write was rejected or accepted depending on which one
// it landed on.
func TestForeignKeysHoldOnEveryConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")
	conn := New("test", path, slog.Default())

	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.db.ExecContext(ctx, `
		CREATE TABLE parents (id TEXT PRIMARY KEY);
		CREATE TABLE children (id TEXT PRIMARY KEY, parent_id TEXT REFERENCES parents(id));
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Force the pool past its first connection, then try the violation on
	// several of them at once.
	var wg sync.WaitGroup
	accepted := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := conn.Write(ctx, &connector.Data{
				Target:    "children",
				Operation: "INSERT",
				Payload: map[string]interface{}{
					"id":        fmt.Sprintf("c%d", i),
					"parent_id": "no-such-parent",
				},
			})
			if err == nil {
				accepted <- struct{}{}
			}
		}(i)
	}
	wg.Wait()
	close(accepted)

	if n := len(accepted); n > 0 {
		t.Errorf("%d writes broke the foreign key and were accepted", n)
	}
}

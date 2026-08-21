package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A constant, against a running service, on both sides of the line.
//
// The whole claim of the block is one name that works everywhere: `${...}` in
// an HCL attribute, which is folded in when the configuration is read, and
// `constants.x` in a CEL expression, which is evaluated per message. They are
// two different machines, and a reader should not have to know which one they
// are writing for — so this exercises one of each in the same service.
func TestAConstantReachesBothTheQueryAndTheCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("starting a service")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	port := freeLocalPort(t)
	adminPort := freeLocalPort(t)

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("constants.mycel", `
constants {
  skus_to_skip = ["SKU-1", "SKU-2"]
  page_size    = 2
}
`)
	write("service.mycel", fmt.Sprintf(`
connector "api" {
  type = "rest"
  port = %d
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = %q
}

service {
  name       = "constants"
  admin_port = %d
}

flow "list_items" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    query     = "SELECT sku FROM items ORDER BY sku LIMIT ${constants.page_size}"
  }
}

flow "keep_item" {
  from {
    connector = "api"
    operation = "POST /items"
  }
  accept {
    when      = "!(input.sku in constants.skus_to_skip)"
    on_reject = "reject"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`, port, dbPath, adminPort))

	seed(t, dbPath,
		`CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, sku TEXT)`,
		`INSERT INTO items (sku) VALUES ('SEED-1'), ('SEED-2'), ('SEED-3')`)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// Start serves until the context is cancelled, so it runs beside the test
	// rather than before it.
	rt, err := startTestRuntime(ctx, dir)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Shutdown() }()
	waitForPort(t, port)

	// HCL: the limit is in the query, folded in when the file was read.
	var listed []map[string]interface{}
	get(t, fmt.Sprintf("http://127.0.0.1:%d/items", port), &listed)
	if len(listed) != 2 {
		t.Errorf("the query returned %d rows, want the %v the constant sets", len(listed), 2)
	}

	// CEL: the accept block reads the same constant, per message.
	if status := post(t, fmt.Sprintf("http://127.0.0.1:%d/items", port), `{"sku":"SKU-1"}`); status >= 500 {
		t.Errorf("a listed sku answered %d", status)
	}
	if status := post(t, fmt.Sprintf("http://127.0.0.1:%d/items", port), `{"sku":"SKU-9"}`); status >= 400 {
		t.Errorf("a sku the constant does not list answered %d", status)
	}

	var after []map[string]interface{}
	get(t, fmt.Sprintf("http://127.0.0.1:%d/items", port), &after)

	stored := storedSKUs(t, dbPath)
	if contains(stored, "SKU-1") {
		t.Errorf("a sku the constant lists was written: %v", stored)
	}
	if !contains(stored, "SKU-9") {
		t.Errorf("a sku the constant does not list was refused: %v", stored)
	}
}

func freeLocalPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("nothing listening on %d", port)
}

func get(t *testing.T, url string, into interface{}) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}

func post(t *testing.T, url, body string) int {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func seed(t *testing.T, dbPath string, statements ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func storedSKUs(t *testing.T, dbPath string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT sku FROM items ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var sku string
		if err := rows.Scan(&sku); err != nil {
			t.Fatal(err)
		}
		out = append(out, sku)
	}
	return out
}

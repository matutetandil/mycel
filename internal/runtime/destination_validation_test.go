package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/parser"
)

// A destination has to say what it writes to.
//
// A `to` block takes whatever it is given: the parser keeps the four attributes
// it knows and sweeps the rest into a bag the connector reads by name. So a
// misspelt attribute is not refused — `targt = "users"` was accepted by
// `mycel validate`, printed in the banner with an empty destination, and
// answered the first request with a SQL syntax error about a table name that
// was never there.
//
// The openness is deliberate and stays. What can still be said is that a
// database destination names either a table or a query, and four examples in
// this repository did neither: three wrote raw SQL under `operation` and one
// wrote a table name there, and all four produced malformed SQL at the first
// request.

func validateConfig(t *testing.T, config string) []error {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.mycel"), []byte(strings.TrimSpace(config)), 0o644); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}

	parsed, err := parser.NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return ValidateFlowSchemas(parsed, NewSchemaRegistry())
}

const oneDatabase = `
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "/tmp/x.db"
}
`

func TestADestinationThatNamesNothingIsRefused(t *testing.T) {
	errs := validateConfig(t, oneDatabase+`
flow "create" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  to {
    connector = "db"
    targt     = "users"
  }
}`)

	if len(errs) == 0 {
		t.Fatal("a destination with a misspelt target was accepted")
	}
	joined := errs[0].Error()
	for _, want := range []string{"create", "db", "target", "query"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the error does not mention %q: %s", want, joined)
		}
	}
}

func TestADestinationThatNamesATableIsAccepted(t *testing.T) {
	for _, written := range []string{`target = "users"`, `query = "SELECT * FROM users"`} {
		t.Run(written, func(t *testing.T) {
			errs := validateConfig(t, oneDatabase+`
flow "read" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    `+written+`
  }
}`)
			if len(errs) != 0 {
				t.Errorf("a destination written as %s was refused: %v", written, errs)
			}
		})
	}
}

func TestATransactionSaysWhatItWritesItself(t *testing.T) {
	// Its statements carry their own tables, so the destination names none.
	errs := validateConfig(t, oneDatabase+`
flow "save" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "db"

    transaction {
      exec {
        query = "INSERT INTO orders (sku) VALUES (:sku)"
        params = { sku = "input.sku" }
      }
    }
  }
}`)

	if len(errs) != 0 {
		t.Errorf("a transactional destination was refused: %v", errs)
	}
}

func TestADestinationThatIsNotADatabaseIsLeftAlone(t *testing.T) {
	// Only a database says it needs a table. A queue's destination is the
	// queue the connector was configured with.
	errs := validateConfig(t, `
connector "api" {
  type = "rest"
  port = 3000
}

connector "rabbit" {
  type     = "mq"
  driver   = "rabbitmq"
  host     = "localhost"
  port     = 5672
  username = "guest"
  password = "guest"
}

flow "publish" {
  from {
    connector = "api"
    operation = "POST /events"
  }
  to {
    connector = "rabbit"
    operation = "PUBLISH"
  }
}`)

	if len(errs) != 0 {
		t.Errorf("a queue destination was refused: %v", errs)
	}
}

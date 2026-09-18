package runtime

import (
	"strings"
	"testing"
)

// conflict_key / on_conflict / ttl describe the record rather than its
// contents, and a store has to be able to act on that: Mongo resolves the
// conflict itself and expires the record through a TTL index, a SQL table does
// neither.
//
// A `to` block is open, so on a connector that cannot act on them the
// attributes would be swept into the connector params and ignored — a file
// saying "the last payload per SKU, kept for a month" while the service
// appends a row per message, forever. That is the class this refuses, and the
// reason it is refused at startup rather than reported per write.

const mongoAndPostgres = `
connector "api" {
  type = "rest"
  port = 3000
}

connector "archive" {
  type     = "database"
  driver   = "mongodb"
  uri      = "mongodb://localhost:27017"
  database = "archive"
}

connector "sql" {
  type     = "database"
  driver   = "postgres"
  host     = "localhost"
  database = "app"
  user     = "app"
  password = "secret"
}
`

func TestMongoAcceptsTheWritePolicy(t *testing.T) {
	errs := validateConfig(t, mongoAndPostgres+`
flow "archive_payload" {
  from {
    connector = "api"
    operation = "POST /skus"
  }

  to {
    connector    = "archive"
    target       = "payload_archive"
    conflict_key = "sku"
    on_conflict  = "replace"
    ttl          = "30d"
  }
}`)

	if len(errs) != 0 {
		t.Fatalf("a Mongo destination was refused its own attributes: %v", errs)
	}
}

func TestASQLDestinationIsToldItCannotActOnTheWritePolicy(t *testing.T) {
	errs := validateConfig(t, mongoAndPostgres+`
flow "archive_payload" {
  from {
    connector = "api"
    operation = "POST /skus"
  }

  to {
    connector    = "sql"
    target       = "payload_archive"
    conflict_key = "sku"
    ttl          = "30d"
  }
}`)

	if len(errs) == 0 {
		t.Fatal("a SQL destination accepted conflict_key and ttl, which it does nothing with")
	}

	joined := errorsText(errs)
	for _, want := range []string{"conflict_key", "ttl", "sql"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the refusal does not mention %q: %s", want, joined)
		}
	}
	// It should point at what to do instead rather than only refusing.
	if !strings.Contains(joined, "query") {
		t.Errorf("the refusal does not say how to write an upsert in SQL: %s", joined)
	}
}

// An aspect's action writes the same way a destination does, and is where an
// archive is usually written from — so it is checked the same way.
func TestAnAspectActionIsCheckedToo(t *testing.T) {
	errs := validateConfig(t, mongoAndPostgres+`
flow "update_sku" {
  from {
    connector = "api"
    operation = "POST /skus"
  }

  response {
    ok = "true"
  }
}

aspect "archive_last_payload" {
  on   = ["update_sku"]
  when = "after"

  action {
    connector    = "sql"
    target       = "payload_archive"
    conflict_key = "sku"
    on_conflict  = "replace"
    ttl          = "30d"
  }
}`)

	if len(errs) == 0 {
		t.Fatal("an aspect action accepted a write policy its connector does nothing with")
	}
	joined := errorsText(errs)
	if !strings.Contains(joined, "archive_last_payload") {
		t.Errorf("the refusal does not name the aspect: %s", joined)
	}
}

func TestAnAspectActionOnMongoKeepsTheWritePolicy(t *testing.T) {
	errs := validateConfig(t, mongoAndPostgres+`
flow "update_sku" {
  from {
    connector = "api"
    operation = "POST /skus"
  }

  response {
    ok = "true"
  }
}

aspect "archive_last_payload" {
  on   = ["update_sku"]
  when = "after"

  action {
    connector    = "archive"
    target       = "payload_archive"
    conflict_key = "sku"
    on_conflict  = "replace"
    ttl          = "30d"

    transform {
      sku  = "string(input.sku)"
      body = "input"
    }
  }
}`)

	if len(errs) != 0 {
		t.Fatalf("an aspect writing to Mongo was refused: %v", errs)
	}
}

func errorsText(errs []error) string {
	var parts []string
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "\n")
}

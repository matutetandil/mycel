package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector/profile"
)

// Reading an attribute with cty's AsString when it is not a string does not
// return an error — it panics. A panic during parsing is the worst answer a
// configuration mistake can get: the binary stops with a Go stack trace, before
// it has said which file, which block or which attribute, and an operator
// reads it as a crash rather than as something they wrote.
//
// A number where a name belongs is an ordinary slip — `port = 8080` next to
// `type = 8080` — and every string attribute has to survive one.

func parseText(t *testing.T, config string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "c.mycel"), []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), dir)
	return err
}

func TestAWrongTypedAttributeDoesNotBringTheBinaryDown(t *testing.T) {
	for name, config := range map[string]string{
		"a connector type as a number": `connector "a" { type = 5 }`,
		"an operation description as a number": `
connector "a" {
  type = "rest"
  operation "get_user" {
    description = 5
    method      = "GET"
    path        = "/users"
  }
}`,
		"an operation path as a number": `
connector "a" {
  type = "rest"
  operation "get_user" {
    method = "GET"
    path   = 404
  }
}`,
		"an operation query as a boolean": `
connector "a" {
  type   = "database"
  driver = "sqlite"
  operation "list" {
    query = true
  }
}`,
		"a profile transform holding a number": `
connector "prices" {
  type    = "http"
  default = "live"
  profile "live" {
    type     = "http"
    base_url = "https://api.example.com"
    transform {
      factor = 2
    }
  }
}`,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("parsing panicked instead of reporting: %v", r)
				}
			}()
			// Whether it is accepted or refused is the next question; what
			// matters here is that it is answered rather than crashed on.
			_ = parseText(t, config)
		})
	}
}

func TestAProfileTransformTakesTheSameValuesAFlowTransformDoes(t *testing.T) {
	// The rule is shared rather than repeated: a quoted CEL expression, or a
	// bare number or boolean, whose literal text is the expression for it.
	cfg := parseOne(t, `
connector "prices" {
  type    = "http"
  default = "live"

  profile "live" {
    type     = "http"
    base_url = "https://api.example.com"

    transform {
      amount   = "input.price * 100"
      currency = "NZD"
      factor   = 2
      active   = true
    }
  }
}
`)

	if len(cfg.Connectors) != 1 {
		t.Fatalf("got %d connectors", len(cfg.Connectors))
	}
	profiles, ok := cfg.Connectors[0].Properties["_profiles"].(*profile.Config)
	if !ok || profiles == nil {
		t.Fatal("the profile block was not read")
	}
	live, found := profiles.Profiles["live"]
	if !found {
		t.Fatalf("profiles = %v", profiles.Profiles)
	}

	for field, want := range map[string]string{
		"amount":   "input.price * 100",
		"currency": "NZD",
		"factor":   "2",
		"active":   "true",
	} {
		if got := live.Transform[field]; got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}

func TestAProfileTransformRefusesWhatHasNoExpression(t *testing.T) {
	err := parseText(t, `
connector "prices" {
  type    = "http"
  default = "live"
  profile "live" {
    type = "http"
    transform {
      fields = ["a", "b"]
    }
  }
}
`)
	if err == nil {
		t.Fatal("a mapping with no expression text was accepted")
	}
	if !strings.Contains(err.Error(), "fields") {
		t.Errorf("error = %q, want it to name the field", err)
	}
}

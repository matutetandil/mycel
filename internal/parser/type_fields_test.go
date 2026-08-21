package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/validate"
)

// What a field in a type block may say.
//
// A type block is a schema: each line names a field and the kind of thing it
// holds. It reads like a record of values, which is why the mistake below is
// an easy one to make — and it used to take the process down.

func parseTypesIn(t *testing.T, body string) ([]*validate.TypeSchema, error) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "types.mycel"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		return nil, err
	}
	return config.Types, nil
}

func TestAFieldWrittenAsAValueRatherThanAType(t *testing.T) {
	// `age = 18` reads like a default, and a type block does not hold
	// defaults — it says a field is a number, not which number. This used to
	// panic on "not a string", taking down `mycel validate` and, during a hot
	// reload, the running service.
	for name, body := range map[string]string{
		"a number": `
type "user" {
  email = string
  age   = 18
}`,
		"a boolean": `
type "user" {
  email  = string
  active = true
}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTypesIn(t, body)
			if err == nil {
				t.Fatal("a value was accepted where a type belongs")
			}
			// The message has to name the field and say what to write
			// instead: the file itself looks perfectly reasonable.
			for _, want := range []string{"age", "active", "string", "number"} {
				if strings.Contains(err.Error(), want) {
					return
				}
			}
			t.Errorf("the error says nothing useful: %v", err)
		})
	}
}

func TestTheWaysAFieldCanNameItsType(t *testing.T) {
	types, err := parseTypesIn(t, `
type "address" {
  city = string
}

type "user" {
  email    = string
  age      = number
  active   = bool
  quoted   = "string"
  home     = address
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("types = %v, want both", types)
	}
}

func TestAFieldWithConstraints(t *testing.T) {
	// The form the parser actually accepts — the arguments are a record
	// inside the call, which is the syntax eighty places in the documentation
	// got wrong for a long time.
	types, err := parseTypesIn(t, `
type "user" {
  email = string({ format = "email" })
  age   = number({ min = 0, max = 150 })
  role  = string({ enum = ["admin", "user"] })
  name  = string({ min_length = 1, max_length = 100 })
  code  = string({ pattern = "^[A-Z]{3}$" })
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("types = %v", types)
	}
}

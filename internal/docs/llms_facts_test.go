package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/connectors"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// docs/llms.txt is the page an assistant reads before answering questions about
// Mycel, and its opening section is a list of facts written to stop one
// inventing syntax. A fact that stops being true there is worse than a stale
// paragraph anywhere else: it is repeated confidently, to somebody who has no
// way to check it, in the exact voice of documentation.
//
// Every claim below is countable, so it is counted rather than believed.

func llmsText(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../docs/llms.txt")
	if err != nil {
		t.Fatalf("reading llms.txt: %v", err)
	}
	return string(body)
}

func TestTheFactsInLLMsTxtAreCountedFromTheCode(t *testing.T) {
	text := llmsText(t)

	for name, tc := range map[string]struct {
		pattern string // a regexp with one capture holding the number
		want    int
	}{
		"the blocks a flow can hold": {
			`anatomy table of all (\d+) blocks`,
			len(parser.FlowBlockNames()),
		},
		"the attributes a flow can hold": {
			`blocks and (\d+) attributes a flow can hold`,
			len(parser.FlowAttributeNames()),
		},
		"the inline blocks that can be named": {
			`Declare any of (\w+) inline blocks`,
			len(parser.ReusableKindNames()),
		},
		"the source connectors": {
			`for each of the (\d+) source types`,
			countSourceTypes(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			claimed, ok := numberIn(text, tc.pattern)
			if !ok {
				t.Fatalf("llms.txt no longer says anything matching %q, so this fact "+
					"cannot be checked — update the pattern or the file", tc.pattern)
			}
			if claimed != tc.want {
				t.Errorf("llms.txt says %d and the code has %d", claimed, tc.want)
			}
		})
	}
}

func TestTheClaimsAboutSyntaxHold(t *testing.T) {
	text := llmsText(t)

	// Each of these is a sentence in llms.txt and a document that proves it.
	// The sentence being there is checked too: a fact that quietly disappears
	// leaves an assistant with nothing, which is its own kind of wrong.
	for name, tc := range map[string]struct {
		says   string
		doc    string
		parses bool
	}{
		"an output field is named on the left, and output. is a parse error": {
			says: "`output.` is never written on the left",
			doc: `flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    output.id = "uuid()"
  }
}`,
			parses: false,
		},
		"the same transform without the prefix": {
			says: "Output fields are named by the **left-hand side**",
			doc: `flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    id = "uuid()"
  }
}`,
			parses: true,
		},
		"a constraint goes in braces inside parentheses": {
			says: "`string({ format = \"email\" })`",
			doc: `type "user" {
  email = string({ format = "email" })
}`,
			parses: true,
		},
		// The two shapes llms.txt names: a CEL string literal, which is
		// single-quoted, and a CEL macro, which is not HCL syntax. Something
		// like lower(input.email) happens to be valid in both languages and
		// does go through unquoted — which is why the claim is about most CEL
		// rather than all of it, and why the example has to be one of these.
		"a CEL string literal unquoted is refused": {
			says: "always written as **quoted strings**",
			doc: `flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    status = 'pending'
  }
}`,
			parses: false,
		},
		"a CEL macro unquoted is refused": {
			says: "CEL macros are not HCL syntax",
			doc: `flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    big = input.items.filter(i, i.price > 10)
  }
}`,
			parses: false,
		},
		"and quoted, both are fine": {
			says: "`email = \"lower(input.email)\"`",
			doc: `flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    status = "'pending'"
    big    = "input.items.filter(i, i.price > 10)"
  }
}`,
			parses: true,
		},
		"a flow needs nothing but from": {
			says: "`from` is the only required block",
			doc: `flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
}`,
			parses: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(text, tc.says) {
				t.Errorf("llms.txt no longer says %q, so an assistant reading it "+
					"has lost this fact", tc.says)
			}
			err := parseBlock(tc.doc)
			switch {
			case tc.parses && err != "":
				t.Errorf("llms.txt says this works and the parser refuses it: %v\n\n%s", err, tc.doc)
			case !tc.parses && err == "":
				t.Errorf("llms.txt says this is refused and the parser accepts it:\n\n%s", tc.doc)
			}
		})
	}
}

func TestTheFileNameClaimHolds(t *testing.T) {
	// ".mycel, not .hcl" is the first thing it says, and the one an assistant
	// is likeliest to get wrong from training data written before 1.18.
	text := llmsText(t)
	if !strings.Contains(text, "`.mycel`") || !strings.Contains(text, "not `.hcl`") {
		t.Fatal("llms.txt no longer states the file extension")
	}

	dir := t.TempDir()
	body := []byte(`connector "api" {
  type = "rest"
  port = 8080
}`)
	if err := os.WriteFile(dir+"/service.hcl", body, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parser.NewHCLParser().Parse(t.Context(), dir)
	if err != nil {
		t.Fatalf("parsing a directory: %v", err)
	}
	if len(cfg.Connectors) != 0 {
		t.Errorf("a .hcl file was read, so the claim that only .mycel is loaded is wrong")
	}
}

// countSourceTypes counts the connector types that can be a flow's source.
//
// A type counts when any of its drivers describes a source: `database` is one
// source type whether the driver is postgres or mongo.
func countSourceTypes() int {
	reg := schema.NewRegistryWith(connectors.RegisterAll)

	n := 0
	for _, connType := range reg.AllConnectorTypes() {
		if hasSource(reg, connType) {
			n++
		}
	}
	return n
}

func hasSource(reg *schema.Registry, connType string) bool {
	// The driverless entry first, then the drivers a type can have: Lookup
	// falls back to the type when a driver is not registered on its own.
	if p := reg.Lookup(connType, ""); p != nil && p.SourceSchema() != nil {
		return true
	}
	for _, driver := range knownDrivers[connType] {
		if p := reg.Lookup(connType, driver); p != nil && p.SourceSchema() != nil {
			return true
		}
	}
	return false
}

// knownDrivers are the types whose schema is registered per driver.
var knownDrivers = map[string][]string{
	"database": {"postgres", "mysql", "sqlite", "mongodb"},
	"mq":       {"rabbitmq", "kafka", "redis"},
	"cache":    {"memory", "redis"},
	"file":     {"local", "s3"},
	"push":     {"fcm", "apns"},
	"sms":      {"twilio", "sns"},
	"email":    {"smtp", "sendgrid", "ses"},
}

// numberIn pulls the number a claim states.
func numberIn(text, pattern string) (int, bool) {
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(m) < 2 {
		return 0, false
	}
	if n, ok := writtenNumbers[strings.ToLower(m[1])]; ok {
		return n, true
	}
	n := 0
	for _, r := range m[1] {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// writtenNumbers covers the counts llms.txt spells out in words.
var writtenNumbers = map[string]int{
	"ten": 10, "eleven": 11, "twelve": 12, "fifteen": 15, "twenty": 20,
	"twenty-one": 21, "four": 4, "five": 5,
}

package ide

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// A name the editor does not check is a name the author finds out about later.
//
// The reference kinds live in the schema, and the editor used to handle four of
// the six with a switch whose default was silence. So a validator that did not
// exist, or a named cache misspelt, drew no squiggle at all, while an undefined
// connector one line above did — and `mycel validate` refused both. These tests
// hold the editor to the schema rather than to whoever last edited the switch.

// everyKindTheSchemaMarks returns the reference kinds actually attached to an
// attribute somewhere in the built-in schemas.
func everyKindTheSchemaMarks(t *testing.T) map[RefKind][]string {
	t.Helper()

	marked := make(map[RefKind][]string)
	var walk func(where string, b schema.Block)
	walk = func(where string, b schema.Block) {
		for _, attr := range b.Attrs {
			if attr.Ref != schema.RefNone {
				marked[attr.Ref] = append(marked[attr.Ref], where+"."+attr.Name)
			}
		}
		for _, child := range b.Children {
			walk(where+"."+child.Type, child)
		}
	}
	for _, root := range schema.BuiltinRootSchemas() {
		walk(root.Type, root)
	}

	if len(marked) == 0 {
		t.Fatal("no attribute in the schema is marked as a reference; this test is checking nothing")
	}
	return marked
}

func TestTheEditorChecksEveryKindOfReferenceTheSchemaMarks(t *testing.T) {
	for kind, attrs := range everyKindTheSchemaMarks(t) {
		if _, handled := referenceKinds[kind]; !handled {
			t.Errorf("the schema marks %d attribute(s) as reference kind %v (for instance %s), "+
				"and the editor does not check that kind, so a name that resolves to nothing "+
				"is reported by `mycel validate` and not by the editor",
				len(attrs), kind, attrs[0])
		}
	}
}

func TestTheEditorReportsAReferenceThatResolvesToNothing(t *testing.T) {
	// One case per kind the table knows, written the way an author would write
	// it, so that the lookup, the prefix and the wording are all exercised.
	cases := []struct {
		kind   RefKind
		config string
		want   string
	}{
		{RefConnector, `
flow "f" {
  from { connector = "nowhere" }
  to   { connector = "db" }
}`, `undefined connector "nowhere"`},
		{RefLock, `
flow "f" {
  from { connector = "db" }
  lock { use = "lock.not_written" }
  to   { connector = "db" }
}`, `undefined lock block "lock.not_written"`},
		{RefDedupe, `
flow "f" {
  from   { connector = "db" }
  dedupe { use = "not_written" }
  to     { connector = "db" }
}`, `undefined dedupe block "not_written"`},
		{RefCache, `
flow "f" {
  from  { connector = "db" }
  cache { use = "cache.not_written" }
  to    { connector = "db" }
}`, `undefined cache "cache.not_written"`},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			idx := newProjectIndex()
			idx.Connectors["db"] = &NamedEntity{Name: "db"}

			fi := parseHCL("test.mycel", []byte(strings.TrimSpace(tc.config)))

			var found bool
			for _, b := range fi.Blocks {
				for _, d := range validateBlockRefs("test.mycel", b, idx) {
					if d.Message == tc.want {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("the editor said nothing about a reference that resolves to nothing; want %q", tc.want)
			}
		})
	}
}

func TestTheEditorAcceptsAReferenceThatResolves(t *testing.T) {
	// The other direction, and the one that matters more: a check that flags a
	// working configuration is worse than one that misses a typo.
	idx := newProjectIndex()
	idx.Connectors["db"] = &NamedEntity{Name: "db"}
	idx.Caches["short"] = &NamedEntity{Name: "short"}
	idx.Named["lock"] = map[string]*NamedEntity{"per_order": {Name: "per_order"}}
	idx.Named["dedupe"] = map[string]*NamedEntity{"standard": {Name: "standard"}}

	const config = `
flow "f" {
  from   { connector = "db" }
  cache  { use = "cache.short" }
  lock   { use = "lock.per_order" }
  dedupe { use = "standard" }
  to     { connector = "db" }
}`

	fi := parseHCL("test.mycel", []byte(strings.TrimSpace(config)))
	for _, b := range fi.Blocks {
		for _, d := range validateBlockRefs("test.mycel", b, idx) {
			t.Errorf("the editor flagged a reference that resolves: %s", d.Message)
		}
	}
}

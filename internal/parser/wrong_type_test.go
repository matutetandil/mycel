package parser

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// What happens when an attribute holds the wrong kind of value.
//
// The parser reads most attributes with a bare cty AsString, which panics on
// anything that is not text — so `cache { ttl = 300 }`, a number where a
// duration string was wanted, took the whole binary down with "panic: not a
// string" during `mycel validate`. That is a plausible thing to write: 300 is
// what somebody means by five minutes, and the failure names neither the
// attribute nor the block.
//
// This walks the schema and puts a number in every attribute declared as text,
// one at a time, asking only that the parser not die. Refusing the value is
// the right answer; so is accepting it and reading it as "300". Crashing is
// not.
//
// It is the third parity test in this package, and the same shape as the other
// two: the schema says what the language contains, and the test asks the real
// parser whether that is true.
func TestNoAttributeCanCrashTheParser(t *testing.T) {
	for _, blk := range schema.BuiltinRootSchemas() {
		blk := blk
		t.Run(blk.Type, func(t *testing.T) {
			for _, probe := range wrongTypeProbes(blk, blk.Type) {
				probe := probe
				t.Run(probe.where, func(t *testing.T) {
					doc := probe.doc
					_, err := tryParse(t, doc)
					if err != nil && strings.HasPrefix(err.Error(), "panic:") {
						t.Errorf("%s took the parser down: %v\n\n%s", probe.where, err, doc)
					}
				})
			}
		})
	}
}

type wrongTypeProbe struct {
	where string // block.attribute, for the failure message
	doc   string
}

// wrongTypeProbes renders one document per string-typed attribute, with a
// number in place of the text.
func wrongTypeProbes(blk schema.Block, name string) []wrongTypeProbe {
	var probes []wrongTypeProbe

	labels := make([]string, blk.Labels)
	for i := range labels {
		labels[i] = name
	}

	for _, path := range stringAttrPaths(blk, nil) {
		doc := renderWithOverride(blk, labels, 0, path, "1234")
		probes = append(probes, wrongTypeProbe{
			where: strings.Join(append([]string{blk.Type}, path...), "."),
			doc:   doc,
		})
	}
	return probes
}

// stringAttrPaths lists every text attribute in a block and its children, as a
// path of block types ending in the attribute name.
func stringAttrPaths(blk schema.Block, prefix []string) [][]string {
	var paths [][]string

	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	for _, a := range attrs {
		// Only the ones that would reach AsString: a declared enum is rendered
		// as its first value and is covered by the parity test already.
		if a.Type != schema.TypeString && a.Type != schema.TypeDuration {
			continue
		}
		if len(a.Values) > 0 {
			continue
		}
		if skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		paths = append(paths, append(append([]string{}, prefix...), a.Name))
	}

	children := append([]schema.Block(nil), blk.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].Type < children[j].Type })
	for _, child := range children {
		paths = append(paths, stringAttrPaths(child, append(append([]string{}, prefix...), child.Type))...)
	}
	return paths
}

// renderWithOverride renders the block the way the parity test does, but with
// one attribute holding the given literal instead of its sample value.
func renderWithOverride(blk schema.Block, labels []string, depth int, path []string, literal string) string {
	var b strings.Builder
	b.WriteString(blockHeader(blk, labels) + " {\n")
	indent := strings.Repeat("  ", depth+1)

	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	for _, a := range attrs {
		if skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		value := sampleValue(a)
		// The override applies when this block is where the path ends.
		if len(path) == 1 && path[0] == a.Name {
			value = literal
		}
		fmt.Fprintf(&b, "%s%s = %s\n", indent, a.Name, value)
	}

	children := append([]schema.Block(nil), blk.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].Type < children[j].Type })
	for _, child := range children {
		childLabels := make([]string, child.Labels)
		for i := range childLabels {
			childLabels[i] = "example"
		}
		childPath := path
		if len(path) > 1 && path[0] == child.Type {
			childPath = path[1:]
		} else {
			childPath = nil
		}
		rendered := renderWithOverride(child, childLabels, depth+1, childPath, literal)
		for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
			b.WriteString(indent + line + "\n")
		}
	}

	b.WriteString(strings.Repeat("  ", depth) + "}\n")
	return b.String()
}

// And the other half: a single value where a list was wanted.
//
// `on = "create_*"` instead of `on = ["create_*"]` is the mistake somebody
// makes once per language they learn, and the parser reads these with a guard
// — `if val.Type().IsListType() || ...` — whose else branch does nothing at
// all. So the attribute is accepted, left empty, and the aspect matches
// nothing, the cache invalidates nothing, the list is simply not there.
//
// Silence is the failure here rather than a crash: the service starts and does
// less than it was told to.
func TestASingleValueWhereAListWasWantedIsNotIgnored(t *testing.T) {
	for _, blk := range schema.BuiltinRootSchemas() {
		blk := blk
		t.Run(blk.Type, func(t *testing.T) {
			for _, probe := range listProbes(blk, blk.Type) {
				probe := probe
				t.Run(probe.where, func(t *testing.T) {
					cfg, err := tryParse(t, probe.doc)
					if err != nil {
						if strings.HasPrefix(err.Error(), "panic:") {
							t.Errorf("%s took the parser down: %v\n\n%s", probe.where, err, probe.doc)
						}
						// Refusing it is a fine answer.
						return
					}
					// Accepted. The value has to have gone somewhere: if the
					// document parses and the attribute is empty, the parser
					// read the line and threw it away.
					if cfg == nil {
						t.Fatalf("%s parsed to nothing", probe.where)
					}
					if !probe.landed(cfg) {
						t.Errorf("%s was accepted and then discarded, so the service runs with the setting missing\n\n%s",
							probe.where, probe.doc)
					}
				})
			}
		})
	}
}

type listProbe struct {
	where  string
	doc    string
	landed func(*Configuration) bool
}

// listProbes renders one document per list attribute, holding a bare string.
func listProbes(blk schema.Block, name string) []listProbe {
	var probes []listProbe

	labels := make([]string, blk.Labels)
	for i := range labels {
		labels[i] = name
	}

	for _, path := range listAttrPaths(blk, nil) {
		where := strings.Join(append([]string{blk.Type}, path...), ".")
		check, known := listLanded[where]
		if !known {
			// Only the ones whose destination this test knows how to look at.
			// A probe that cannot check where the value went would pass for
			// the wrong reason.
			continue
		}
		probes = append(probes, listProbe{
			where:  where,
			doc:    renderWithOverride(blk, labels, 0, path, `"one_value"`),
			landed: check,
		})
	}
	return probes
}

func listAttrPaths(blk schema.Block, prefix []string) [][]string {
	var paths [][]string

	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	for _, a := range attrs {
		if a.Type != schema.TypeList || skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		paths = append(paths, append(append([]string{}, prefix...), a.Name))
	}

	children := append([]schema.Block(nil), blk.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].Type < children[j].Type })
	for _, child := range children {
		paths = append(paths, listAttrPaths(child, append(append([]string{}, prefix...), child.Type))...)
	}
	return paths
}

// listLanded says, for each list attribute this test covers, how to see
// whether the value reached the configuration.
var listLanded = map[string]func(*Configuration) bool{
	"aspect.on": func(c *Configuration) bool {
		return len(c.Aspects) > 0 && len(c.Aspects[0].On) > 0
	},
	"flow.cache.invalidate_on": func(c *Configuration) bool {
		return len(c.Flows) > 0 && c.Flows[0].Cache != nil && len(c.Flows[0].Cache.InvalidateOn) > 0
	},
	"flow.require.roles": func(c *Configuration) bool {
		return len(c.Flows) > 0 && c.Flows[0].Require != nil && len(c.Flows[0].Require.Roles) > 0
	},
	"flow.require.permissions": func(c *Configuration) bool {
		return len(c.Flows) > 0 && c.Flows[0].Require != nil && len(c.Flows[0].Require.Permissions) > 0
	},
}

// The ones the schema does not describe, written out by hand: a list attribute
// left out of the schema is invisible to the loop above, and these are the
// attributes where a discarded value is most expensive — a cache that is never
// invalidated serves what it has until the TTL, and a rule that requires no
// roles requires nothing.
func TestASingleNameReachesTheSettingItWasWrittenIn(t *testing.T) {
	for name, tc := range map[string]struct {
		doc    string
		landed func(*Configuration) bool
	}{
		"a cache invalidated by one flow": {
			`connector "api" {
  type = "rest"
  port = 8080
}
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    storage       = "memcache"
    ttl           = "5m"
    invalidate_on = "update_user"
  }
}`,
			func(c *Configuration) bool {
				return c.Flows[0].Cache != nil && len(c.Flows[0].Cache.InvalidateOn) == 1 &&
					c.Flows[0].Cache.InvalidateOn[0] == "update_user"
			},
		},
		"a rule requiring one role": {
			`connector "api" {
  type = "rest"
  port = 8080
}
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  require {
    roles = "admin"
  }
}`,
			func(c *Configuration) bool {
				return c.Flows[0].Require != nil && len(c.Flows[0].Require.Roles) == 1 &&
					c.Flows[0].Require.Roles[0] == "admin"
			},
		},
		"an aspect matching one flow": {
			`aspect "audit" {
  on   = "create_order"
  when = "after"
  action {
    connector = "audit_db"
    operation = "INSERT"
  }
}`,
			func(c *Configuration) bool {
				return len(c.Aspects) == 1 && len(c.Aspects[0].On) == 1 &&
					c.Aspects[0].On[0] == "create_order"
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := tryParse(t, tc.doc)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !tc.landed(cfg) {
				t.Error("the value was accepted and discarded, so the service runs without the setting")
			}
		})
	}
}

// And a list still reads as a list, which is the spelling everything in the
// documentation uses.
func TestAListOfNamesStillReadsAsOne(t *testing.T) {
	cfg, err := tryParse(t, `connector "api" {
  type = "rest"
  port = 8080
}
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    storage       = "memcache"
    ttl           = "5m"
    invalidate_on = ["update_user", "delete_user"]
  }
  require {
    roles = ["admin", "support"]
  }
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Flows[0].Cache.InvalidateOn; len(got) != 2 || got[1] != "delete_user" {
		t.Errorf("invalidate_on = %v", got)
	}
	if got := cfg.Flows[0].Require.Roles; len(got) != 2 || got[0] != "admin" {
		t.Errorf("roles = %v", got)
	}
}

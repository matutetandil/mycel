package parser

import (
	"fmt"
	"sort"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/connectors"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Connector schemas are what completions, `mycel add` and the exported
// documentation are built from, and nothing ever checked them against the
// parser. That is how the gRPC connector came to advertise six TLS attributes
// the parser refused, and how named operations stayed missing from the schema
// altogether while being parsed, documented and exampled.
//
// This writes each attribute a schema declares into a connector block on its
// own and hands it to the real parser, so a failure names one attribute rather
// than the first of a pile.
//
// knownDrift records attributes a schema declares that the parser does not
// accept. It is empty, and the intention is that it stays that way.
//
// It held 38 entries across 12 connectors when this test was written — the
// parser keeps a hand-written allow-list and it had fallen behind the schemas.
// Every one of them turned out to be read by its connector, so all 38 were
// settings that were implemented, described by the schema, offered as
// completions, and impossible to write. They were fixed rather than accepted.
//
// If something has to go in here, it belongs with a reason and a plan. An
// entry that starts parsing fails the test, so the list cannot go stale.
var knownDrift = map[string][]string{}

func TestConnectorSchemasMatchTheParser(t *testing.T) {
	reg := schema.NewRegistryWith(connectors.RegisterAll)

	types := reg.AllConnectorTypes()
	sort.Strings(types)

	for _, connType := range types {
		provider := reg.Lookup(connType, "")
		if provider == nil {
			continue
		}

		t.Run(connType, func(t *testing.T) {
			known := map[string]bool{}
			for _, name := range knownDrift[connType] {
				known[name] = true
			}
			seen := map[string]bool{}

			check := func(name, doc string) {
				seen[name] = true
				_, err := tryParse(t, doc)
				switch {
				case err != nil && !known[name]:
					t.Errorf("the %s schema offers %q, which the parser rejects:\n%v", connType, name, err)
				case err == nil && known[name]:
					t.Errorf("%s %q parses now — remove it from knownDrift", connType, name)
				}
			}

			blk := provider.ConnectorSchema()
			for _, a := range blk.Attrs {
				check(a.Name, fmt.Sprintf("connector \"c\" {\n  type = %q\n  %s = %s\n}\n",
					connType, a.Name, sampleValue(a)))
			}
			for _, child := range blk.Children {
				for _, a := range child.Attrs {
					check(child.Type+"."+a.Name,
						fmt.Sprintf("connector \"c\" {\n  type = %q\n  %s {\n    %s = %s\n  }\n}\n",
							connType, child.Type, a.Name, sampleValue(a)))
				}
				if len(child.Attrs) == 0 {
					check(child.Type+" (block)",
						fmt.Sprintf("connector \"c\" {\n  type = %q\n  %s {\n  }\n}\n", connType, child.Type))
				}
			}

			// An entry naming something the schema no longer declares would sit
			// there forever pretending to cover it.
			for _, name := range knownDrift[connType] {
				if !seen[name] {
					t.Errorf("knownDrift lists %s %q, which the schema no longer declares", connType, name)
				}
			}
		})
	}
}

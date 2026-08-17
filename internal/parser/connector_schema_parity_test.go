package parser

import (
	"fmt"
	"sort"
	"strings"
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
			body := scaffold[connType]
			for _, a := range blk.Attrs {
				// The scaffold may already set the attribute under test; HCL
				// refuses the same argument twice, so drop it in that case.
				check(a.Name, fmt.Sprintf("connector \"c\" {\n  type = %q\n%s  %s = %s\n}\n",
					connType, without(body, a.Name), a.Name, sampleValue(a)))
			}
			for _, child := range blk.Children {
				// A block the schema says is named must be written with its
				// name, or the parser rejects the shape rather than the
				// attribute being tested.
				header := child.Type
				if child.Labels > 0 {
					header = child.Type + " \"one\""
				}
				inner := childScaffold[connType+"."+child.Type]
				for _, a := range child.Attrs {
					check(child.Type+"."+a.Name,
						fmt.Sprintf("connector \"c\" {\n  type = %q\n%s  %s {\n%s    %s = %s\n  }\n}\n",
							connType, body, header, without(inner, a.Name), a.Name, sampleValue(a)))
				}
				if len(child.Attrs) == 0 {
					check(child.Type+" (block)",
						fmt.Sprintf("connector \"c\" {\n  type = %q\n%s  %s {\n%s  }\n}\n",
							connType, body, header, inner))
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

// scaffold is what a connector of a given type needs before any attribute can
// be tested at all — rules the parser enforces about the block as a whole
// rather than about one attribute. A profiled connector must name a profile to
// select, or parsing stops before it looks at anything else.
//
// Deliberately as small as possible: anything added here is a rule this test
// stops checking, so it holds only what the parser refuses the block without.
var scaffold = map[string]string{
	"profiled": "  default = \"one\"\n",
}

// childScaffold is the same for a named block inside a connector: a profile is
// a whole connector configuration, so it must say what type it is.
var childScaffold = map[string]string{
	"profiled.profile": "    type = \"http\"\n",
}

// without drops the scaffold line that sets name, so an attribute under test
// is not written twice.
func without(scaffoldLines, name string) string {
	var kept []string
	for _, line := range strings.Split(scaffoldLines, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name+" ") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// acceptedAliases are attributes the parser takes on purpose that no schema
// offers, because each is a second spelling of something already declared.
// They stay valid to write; completions should show the canonical name.
var acceptedAliases = map[string]string{
	"auth_db":  "auth_source", // mongodb
	"ssl_mode": "sslmode",     // postgres
}

// Every registered connector schema, drivers included. Lookup with an empty
// driver returns only the type's default provider, which would leave every
// driver-specific attribute — Kafka's brokers, MySQL's charset — looking
// undeclared.
var registeredPairs = [][2]string{
	{"rest", ""}, {"http", ""}, {"graphql", ""}, {"grpc", ""}, {"tcp", ""}, {"soap", ""},
	{"websocket", ""}, {"sse", ""},
	{"database", ""}, {"database", "postgres"}, {"database", "mysql"},
	{"database", "sqlite"}, {"database", "mongodb"},
	{"mq", ""}, {"mq", "rabbitmq"}, {"mq", "kafka"}, {"mq", "redis"},
	{"file", ""}, {"s3", ""}, {"ftp", ""}, {"exec", ""}, {"cache", ""},
	{"elasticsearch", ""}, {"cdc", ""}, {"pdf", ""}, {"mqtt", ""}, {"oauth", ""},
	{"email", ""}, {"slack", ""}, {"discord", ""}, {"sms", ""}, {"push", ""}, {"webhook", ""},
	{"profiled", ""},
}

// TestTheParserAcceptsNothingUndescribed is the other direction.
//
// An attribute the parser takes and no schema declares is one of two things,
// and both are bugs. Either a connector reads it, and a real setting is
// invisible to completions, `mycel add` and the exported documentation — the
// state named operations were in. Or nothing reads it, and the parser accepts
// a word that quietly does nothing, which is how the exec connector came to
// take `working_dir` from every example while reading `workdir`.
func TestTheParserAcceptsNothingUndescribed(t *testing.T) {
	reg := schema.NewRegistryWith(connectors.RegisterAll)

	declared := map[string]bool{}
	for _, a := range schema.BaseConnectorSchema().Attrs {
		declared[a.Name] = true
	}
	for _, c := range schema.BaseConnectorSchema().Children {
		declared[c.Type] = true
	}
	for _, pair := range registeredPairs {
		provider := reg.Lookup(pair[0], pair[1])
		if provider == nil {
			t.Fatalf("no schema registered for %v", pair)
		}
		blk := provider.ConnectorSchema()
		for _, a := range blk.Attrs {
			declared[a.Name] = true
		}
		// A name written as a nested block is declared just as much.
		for _, c := range blk.Children {
			declared[c.Type] = true
		}
	}

	var undescribed []string
	for _, a := range connectorBodySchema().Attributes {
		if declared[a.Name] {
			continue
		}
		if canonical, isAlias := acceptedAliases[a.Name]; isAlias {
			if !declared[canonical] {
				t.Errorf("%q is listed as an alias of %q, which no schema declares", a.Name, canonical)
			}
			continue
		}
		undescribed = append(undescribed, a.Name)
	}
	sort.Strings(undescribed)

	for _, name := range undescribed {
		t.Errorf("the parser accepts connector attribute %q and no schema declares it: "+
			"either a connector reads it and the schema should say so, or nothing does and the parser should not take it",
			name)
	}
}

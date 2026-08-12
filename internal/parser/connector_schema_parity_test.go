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
// knownDrift records the ones that do not parse today. They are a separate
// problem — the parser keeps a hand-written allow-list of connector attributes
// that has fallen behind the schemas — and each needs deciding on its own:
// either the parser should accept it, or the connector never read it and the
// schema should stop offering it. The list exists so that the drift cannot
// grow while that is worked through, and the test fails if an entry starts
// parsing, so it cannot quietly go stale either.
var knownDrift = map[string][]string{
	"cache":    {"pool.max_connections", "pool.min_idle"},
	"database": {"replicas.host", "replicas.port", "replicas.weight", "replicas.max_connections"},
	"email":    {"template", "tls"},
	"exec":     {"workdir", "env (block)"},
	"file":     {"csv_delimiter", "csv_comment", "csv_no_header", "csv_trim_space", "csv_skip_rows"},
	"ftp":      {"tls"},
	"graphql":  {"cors.allow_credentials"},
	"mq":       {"heartbeat", "reconnect_delay"},
	"oauth":    {"name"},
	"pdf":      {"template", "output_dir", "page_size", "font", "margin_left", "margin_top", "margin_right"},
	"tcp":      {"idle_timeout"},
	"webhook": {"include_timestamp", "require_https", "allowed_ips",
		"retry.max_attempts", "retry.initial_delay", "retry.multiplier"},
}

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

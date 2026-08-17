package runtime

import (
	"sort"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/connectors"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// A connector schema is what tooling offers as completions and what `mycel add`
// generates from. When it names an attribute the parser rejects, the tooling
// leads people to write files that do not parse — and when it names a
// superseded spelling, they are led to the one the connector no longer reads.
//
// The schema parity test covers root blocks only, which is how the gRPC
// connector came to advertise six TLS attributes the parser refused while three
// other connectors read a fourth vocabulary. This closes that hole for tls.
//
// It runs over both registries: the runtime's own, built from the connector
// packages, and the copy in pkg/connectors that external tooling links
// against. They are separate definitions of the same thing and can drift from
// each other as easily as either can drift from the parser.
func TestConnectorTLSSchemasMatchTheParser(t *testing.T) {
	accepted := map[string]bool{}
	for _, name := range parser.CanonicalTLSAttributes() {
		accepted[name] = true
	}
	superseded := parser.SupersededTLSAttributes()
	for alias := range superseded {
		accepted[alias] = true
	}

	registries := map[string]*schema.Registry{
		"runtime":        schema.NewRegistryWith(RegisterBuiltinSchemas),
		"pkg/connectors": schema.NewRegistryWith(connectors.RegisterAll),
	}

	for regName, reg := range registries {
		t.Run(regName, func(t *testing.T) {
			types := reg.AllConnectorTypes()
			sort.Strings(types)

			checked := 0
			for _, connType := range types {
				provider := reg.Lookup(connType, "")
				if provider == nil {
					continue
				}
				for _, child := range provider.ConnectorSchema().Children {
					if child.Type != "tls" {
						continue
					}
					checked++
					for _, attr := range child.Attrs {
						if !accepted[attr.Name] {
							t.Errorf("the %s schema offers tls attribute %q, which the parser rejects",
								connType, attr.Name)
						}
						if canonical, isOld := superseded[attr.Name]; isOld {
							t.Errorf("the %s schema still offers the superseded name %q; completions should show %q",
								connType, attr.Name, canonical)
						}
					}
				}
			}

			// If the walk stops finding tls blocks the test passes vacuously,
			// which is the one way a guard like this rots unnoticed.
			if checked == 0 {
				t.Fatal("no connector schema declared a tls block, so nothing was checked")
			}
		})
	}
}

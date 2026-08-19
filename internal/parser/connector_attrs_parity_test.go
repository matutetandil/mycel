package parser

import (
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/connectors"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Every attribute a connector says it takes, offered to the parser.
//
// The parser holds one hand-written list of connector attributes covering all
// twenty-odd connectors, while each connector describes its own in
// pkg/connectors — the list the documentation, the IDE and `mycel add` are
// generated from. Nothing kept the two in step, so a connector could declare an
// attribute, read it at run time, document it, and have the parser refuse the
// line. That drift was found once before by hand, fifteen attributes at a time.
//
// The failure is not subtle when it happens, since the service does not start.
// It just reaches whoever copied the documentation rather than whoever changed
// the connector.
func TestTheParserAcceptsEveryAttributeAConnectorDeclares(t *testing.T) {
	body := connectorBodySchema()

	accepted := make(map[string]bool, len(body.Attributes))
	for _, a := range body.Attributes {
		accepted[a.Name] = true
	}
	acceptedBlocks := make(map[string]bool, len(body.Blocks))
	for _, b := range body.Blocks {
		acceptedBlocks[b.Type] = true
	}

	reg := schema.NewRegistryWith(connectors.RegisterAll)

	var missing []string

	// The attributes every connector has are checked as well: the base schema
	// carries them so that each connector need not repeat them, which makes
	// them exactly as easy to forget in the parser.
	for _, a := range schema.BaseConnectorSchema().Attrs {
		if !accepted[a.Name] {
			missing = append(missing, "every connector: "+a.Name)
		}
	}
	for _, c := range schema.BaseConnectorSchema().Children {
		if !acceptedBlocks[c.Type] {
			missing = append(missing, "every connector: "+c.Type+" {}")
		}
	}

	for _, pair := range registeredPairs {
		provider := reg.Lookup(pair[0], pair[1])
		if provider == nil {
			t.Fatalf("no schema registered for %v", pair)
		}
		name := pair[0]
		if pair[1] != "" {
			name = pair[0] + "/" + pair[1]
		}
		blk := provider.ConnectorSchema()
		for _, a := range blk.Attrs {
			if !accepted[a.Name] {
				missing = append(missing, name+": "+a.Name)
			}
		}
		for _, child := range blk.Children {
			if !acceptedBlocks[child.Type] {
				missing = append(missing, name+": "+child.Type+" {}")
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these connectors declare something the parser refuses, so a configuration written "+
			"from the documentation does not start:\n  %s", strings.Join(missing, "\n  "))
	}
}

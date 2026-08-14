package parser

import (
	"testing"

	"github.com/matutetandil/mycel/v2/internal/validate"
)

// A type block describes a shape, and in a federated graph it also describes a
// service's claim on that shape: which fields identify an entity, which are
// owned elsewhere, which two subgraphs may both resolve. Getting one of those
// wrong is not a local error — the gateway refuses to compose the graph, or
// composes one where a field resolves from the wrong service.

func typeFrom(t *testing.T, body string) *validate.TypeSchema {
	t.Helper()
	cfg := mustParseProfiles(t, `
type "product" {`+body+`
}
`)
	if len(cfg.Types) != 1 {
		t.Fatalf("%d types parsed", len(cfg.Types))
	}
	return cfg.Types[0]
}

func TestATypeSaysWhatIdentifiesIt(t *testing.T) {
	// The key is what a gateway sends to ask this service for one entity, so a
	// type with none cannot be referenced from another subgraph at all.
	schema := typeFrom(t, `
  _key = "id"

  id   = string
  name = string`)

	if len(schema.Keys) != 1 || schema.Keys[0] != "id" {
		t.Errorf("keys = %v", schema.Keys)
	}
}

func TestAKeyCanBeSeveralFields(t *testing.T) {
	// A line on an order is identified by the order and the line together.
	schema := typeFrom(t, `
  _key = ["order_id", "sku"]

  order_id = string
  sku      = string`)

	if len(schema.Keys) != 2 || schema.Keys[0] != "order_id" || schema.Keys[1] != "sku" {
		t.Errorf("keys = %v", schema.Keys)
	}
}

func TestATypeCanSayWhatItImplementsAndHowItIsShared(t *testing.T) {
	schema := typeFrom(t, `
  _key          = "id"
  _implements   = ["Node", "Timestamped"]
  _shareable    = true
  _inaccessible = false
  _description  = "A product in the catalogue"

  id = string`)

	if len(schema.InterfaceNames) != 2 {
		t.Errorf("interfaces = %v", schema.InterfaceNames)
	}
	if !schema.Shareable {
		t.Error("a type both subgraphs may resolve was not marked shareable")
	}
	if schema.Inaccessible {
		t.Error("a type nothing hid was marked inaccessible")
	}
	if schema.Description == "" {
		t.Error("the description was dropped")
	}
}

func TestASingleInterfaceNeedNotBeAList(t *testing.T) {
	schema := typeFrom(t, `
  _implements = "Node"

  id = string`)

	if len(schema.InterfaceNames) != 1 || schema.InterfaceNames[0] != "Node" {
		t.Errorf("interfaces = %v", schema.InterfaceNames)
	}
}

func TestAFieldSaysWhoOwnsIt(t *testing.T) {
	// external means another subgraph owns this field and this one only refers
	// to it; requires names what this service needs before it can resolve its
	// own. Both are what the gateway plans a query from.
	schema := typeFrom(t, `
  _key = "id"

  id     = string
  price  = number({ external = true })
  margin = number({ requires = "price" })
  code   = string({ provides = "sku" })`)

	fields := map[string]*validate.FieldSchema{}
	for i := range schema.Fields {
		fields[schema.Fields[i].Name] = &schema.Fields[i]
	}

	if !fields["price"].External {
		t.Error("a field owned by another subgraph was not marked external")
	}
	if fields["margin"].Requires != "price" {
		t.Errorf("requires = %q", fields["margin"].Requires)
	}
	if fields["code"].Provides != "sku" {
		t.Errorf("provides = %q", fields["code"].Provides)
	}
}

func TestAFieldCanBeMovedBetweenSubgraphs(t *testing.T) {
	// override is how a field is migrated from one service to another without
	// a flag day: this subgraph takes it over from the one named.
	schema := typeFrom(t, `
  _key = "id"

  id    = string
  stock = number({ override = "inventory-service", shareable = true })
  cost  = number({ inaccessible = true, description = "internal only" })`)

	fields := map[string]*validate.FieldSchema{}
	for i := range schema.Fields {
		fields[schema.Fields[i].Name] = &schema.Fields[i]
	}

	if fields["stock"].Override != "inventory-service" {
		t.Errorf("override = %q", fields["stock"].Override)
	}
	if !fields["stock"].Shareable {
		t.Error("a field both subgraphs may resolve was not marked shareable")
	}
	if !fields["cost"].Inaccessible {
		t.Error("a field hidden from the public graph was not marked inaccessible")
	}
	if fields["cost"].Description == "" {
		t.Error("the field description was dropped")
	}
}

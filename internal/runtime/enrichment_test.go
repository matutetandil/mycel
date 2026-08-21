package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/transform"
)

// An enrichment fetches what a message did not carry — the customer behind an
// id, the price behind a code — and makes it available to the transform as
// `enriched.<name>`. It is the piece that turns a thin event into a record
// somebody can act on, so a lookup that quietly returns nothing produces a
// record with a hole in it.

// tableConnector stands in for a database: it can be read and not called,
// which is what every database driver in the tree is. The fake used here
// before could do both, so three tests that meant "a lookup against a table"
// were quietly exercising the call path instead.
type tableConnector struct {
	name    string
	rows    []map[string]interface{}
	err     error
	filters map[string]interface{}
}

func (c *tableConnector) Name() string                  { return c.name }
func (c *tableConnector) Type() string                  { return "database" }
func (c *tableConnector) Connect(context.Context) error { return nil }
func (c *tableConnector) Close(context.Context) error   { return nil }
func (c *tableConnector) Health(context.Context) error  { return nil }

func (c *tableConnector) Read(_ context.Context, q connector.Query) (*connector.Result, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.filters = q.Filters
	return &connector.Result{Rows: c.rows}, nil
}

func enrichingHandler(t *testing.T, enrichments []*flow.EnrichConfig, conns map[string]connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	for name, conn := range conns {
		registry.Replace(name, conn)
	}
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return &FlowHandler{
		Config:      &flow.Config{Name: "enrich_order"},
		Connectors:  registry,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestALookupIsAvailableUnderItsName(t *testing.T) {
	customers := &tableConnector{name: "db", rows: []map[string]interface{}{
		{"id": "c-1", "email": "someone@example.com", "tier": "gold"},
	}}

	h := enrichingHandler(t, nil, map[string]connector.Connector{"db": customers})
	enriched, err := h.executeEnrichments(context.Background(),
		map[string]interface{}{"customer_id": "c-1"},
		[]*flow.EnrichConfig{{
			Name: "customer", Connector: "db",
			Params:          map[string]string{"id": "input.customer_id"},
			ConnectorParams: map[string]interface{}{"operation": "customers"},
		}})
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}

	// A single row is the row itself, so a transform reads enriched.customer.tier
	// rather than enriched.customer[0].tier.
	customer, ok := enriched["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("customer = %#v, want the row itself", enriched["customer"])
	}
	if customer["tier"] != "gold" {
		t.Errorf("customer = %v", customer)
	}

	// And the lookup was made with what the message carried.
	if customers.filters["id"] != "c-1" {
		t.Errorf("the lookup asked for %v, want the id from the message", customers.filters)
	}
}

func TestSeveralRowsComeBackAsAListToo(t *testing.T) {
	db := &tableConnector{name: "db", rows: []map[string]interface{}{{"id": "1"}, {"id": "2"}}}
	h := enrichingHandler(t, nil, map[string]connector.Connector{"db": db})

	enriched, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{
			Name: "orders", Connector: "db",
			ConnectorParams: map[string]interface{}{"operation": "orders"},
		}})
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}
	rows, ok := enriched["orders"].([]map[string]interface{})
	if !ok || len(rows) != 2 {
		t.Errorf("orders = %#v, want both rows", enriched["orders"])
	}
}

func TestSeveralLookupsEachGetTheirOwnName(t *testing.T) {
	customers := &tableConnector{name: "db", rows: []map[string]interface{}{{"id": "c-1", "tier": "gold"}}}
	// Call-only, because a connector that can also be read from is read from:
	// an enrichment is a lookup, so the read path is preferred deliberately.
	prices := &callOnlyConnector{answer: map[string]interface{}{"amount": 42}}

	h := enrichingHandler(t, nil, map[string]connector.Connector{"db": customers, "pricing": prices})
	enriched, err := h.executeEnrichments(context.Background(),
		map[string]interface{}{"customer_id": "c-1", "sku": "A-1"},
		[]*flow.EnrichConfig{
			{
				Name: "customer", Connector: "db",
				Params:          map[string]string{"id": "input.customer_id"},
				ConnectorParams: map[string]interface{}{"operation": "customers"},
			},
			{
				Name: "price", Connector: "pricing",
				Params:          map[string]string{"sku": "input.sku"},
				ConnectorParams: map[string]interface{}{"operation": "GET /price"},
			},
		})
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}

	if enriched["customer"] == nil || enriched["price"] == nil {
		t.Fatalf("enriched = %v, want both lookups", enriched)
	}
	// Each under its own name, or a transform cannot tell them apart.
	price, _ := enriched["price"].(map[string]interface{})
	if price == nil || price["amount"] != 42 {
		t.Errorf("price = %v", enriched["price"])
	}
}

func TestALookupThatFailsStopsTheFlowAndSaysWhich(t *testing.T) {
	// Enrichment is data the record needs; carrying on without it would send
	// an incomplete record onward, which is worse than not sending one. The
	// name matters because a flow may have several.
	broken := &tableConnector{name: "db", err: errors.New("connection refused")}
	h := enrichingHandler(t, nil, map[string]connector.Connector{"db": broken})

	_, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{
			Name: "customer", Connector: "db",
			ConnectorParams: map[string]interface{}{"operation": "customers"},
		}})
	if err == nil {
		t.Fatal("a failed lookup was ignored")
	}
	if !strings.Contains(err.Error(), "customer") {
		t.Errorf("error = %q, want it to name the enrichment", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want the reason underneath", err)
	}
}

func TestALookupNamingAConnectorThatDoesNotExistIsReported(t *testing.T) {
	h := enrichingHandler(t, nil, nil)
	_, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{Name: "customer", Connector: "typo"}})
	if err == nil {
		t.Fatal("a connector that does not exist was accepted")
	}
	for _, want := range []string{"customer", "typo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestAParameterThatCannotBeEvaluatedIsReported(t *testing.T) {
	// Naming a field the message does not carry is a configuration mistake,
	// and it has to say which parameter of which lookup.
	db := &tableConnector{name: "db"}
	h := enrichingHandler(t, nil, map[string]connector.Connector{"db": db})

	_, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{
			Name: "customer", Connector: "db",
			Params:          map[string]string{"id": "input.absent.nested"},
			ConnectorParams: map[string]interface{}{"operation": "customers"},
		}})
	if err == nil {
		t.Fatal("a parameter that cannot be evaluated was accepted")
	}
	if !strings.Contains(err.Error(), "customer") || !strings.Contains(err.Error(), "id") {
		t.Errorf("error = %q, want it to name the enrichment and the parameter", err)
	}
}

func TestAConnectorThatCanDoNeitherIsNamed(t *testing.T) {
	registry := connector.NewRegistry()
	registry.Replace("bare", &bareRuntimeConnector{})
	tr, _ := transform.NewCELTransformer()
	h := &FlowHandler{
		Config: &flow.Config{Name: "f"}, Connectors: registry, Transformer: tr,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	_, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{Name: "customer", Connector: "bare"}})
	if err == nil {
		t.Fatal("a connector that can neither read nor be called was accepted")
	}
	if !strings.Contains(err.Error(), "bare") {
		t.Errorf("error = %q, want the connector named", err)
	}
}

func TestNoEnrichmentsIsNothingToDo(t *testing.T) {
	h := enrichingHandler(t, nil, nil)
	enriched, err := h.executeEnrichments(context.Background(), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}
	if len(enriched) != 0 {
		t.Errorf("enriched = %v, want nothing", enriched)
	}
}

type bareRuntimeConnector struct{}

func (bareRuntimeConnector) Name() string                  { return "bare" }
func (bareRuntimeConnector) Type() string                  { return "bare" }
func (bareRuntimeConnector) Connect(context.Context) error { return nil }
func (bareRuntimeConnector) Close(context.Context) error   { return nil }
func (bareRuntimeConnector) Health(context.Context) error  { return nil }

// callOnlyConnector can be called and not read, which is how a connector with
// no query interface takes part in an enrichment.
type callOnlyConnector struct {
	answer interface{}
}

func (callOnlyConnector) Name() string                  { return "pricing" }
func (callOnlyConnector) Type() string                  { return "fake" }
func (callOnlyConnector) Connect(context.Context) error { return nil }
func (callOnlyConnector) Close(context.Context) error   { return nil }
func (callOnlyConnector) Health(context.Context) error  { return nil }

func (c callOnlyConnector) Call(context.Context, string, map[string]interface{}) (interface{}, error) {
	return c.answer, nil
}

func TestAConnectorThatCanDoBothIsCalledRatherThanRead(t *testing.T) {
	// The preference used to be the other way round, on the grounds that a
	// read carries its parameters as filters. It does — but every connector
	// that only reads is a database, and those take the read path regardless;
	// the preference only decides for the ones that do both, and there the
	// read path was losing information.
	//
	// A GraphQL field holding a list of one came back as the object and the
	// same field holding two came back as the list, because the read path
	// flattens a response into rows and then a single row is unwrapped. A REST
	// gateway forwarding that answered a different shape depending on how many
	// rows happened to exist upstream. The call path hands back what the
	// service actually said, which is also the shape its own documentation
	// describes: enriched.<name> is the answer, whatever shape that is.
	both := &stepConnector{
		name: "api",
		rows: []map[string]interface{}{{"from": "read"}},
		call: map[string]interface{}{"from": "call"},
	}

	h := enrichingHandler(t, nil, map[string]connector.Connector{"api": both})
	enriched, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{
			Name: "lookup", Connector: "api",
			ConnectorParams: map[string]interface{}{"operation": "GET /customers"},
		}})
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}

	row, _ := enriched["lookup"].(map[string]interface{})
	if row == nil || row["from"] != "call" {
		t.Errorf("the enrichment came from %v, want the call path", enriched["lookup"])
	}
}

// A list of one stays a list, which is the whole reason for the preference.
func TestAnEnrichmentDoesNotFlattenAListOfOne(t *testing.T) {
	upstream := &stepConnector{
		name: "api",
		call: map[string]interface{}{"products": []interface{}{
			map[string]interface{}{"id": "p1"},
		}},
	}

	h := enrichingHandler(t, nil, map[string]connector.Connector{"api": upstream})
	enriched, err := h.executeEnrichments(context.Background(), map[string]interface{}{},
		[]*flow.EnrichConfig{{
			Name: "catalogue", Connector: "api",
			ConnectorParams: map[string]interface{}{"operation": "query { products { id } }"},
		}})
	if err != nil {
		t.Fatalf("executeEnrichments: %v", err)
	}

	answer, _ := enriched["catalogue"].(map[string]interface{})
	if answer == nil {
		t.Fatalf("the enrichment came back as %#v", enriched["catalogue"])
	}
	products, ok := answer["products"].([]interface{})
	if !ok || len(products) != 1 {
		t.Errorf("products came back as %#v, want a list of one", answer["products"])
	}
}

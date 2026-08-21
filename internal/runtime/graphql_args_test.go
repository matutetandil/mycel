package runtime

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// What arguments a GraphQL field publishes.
//
// A query flow that fetches with `query = "... WHERE sku = :sku"` names its
// parameter there and nowhere else, and only a step's params were read — so the
// field was published taking no arguments, and asking for
// `product(sku: "ABC-123")`, which is what the federation example's README
// shows, was answered "Unknown argument sku".

func argTypes(args []*ArgDef) map[string]string {
	out := map[string]string{}
	for _, a := range args {
		out[a.Name] = a.Type
	}
	return out
}

func argNames(args []*ArgDef) []string {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = a.Name
	}
	return names
}

func TestAQueryPublishesTheArgumentsItsDestinationNames(t *testing.T) {
	cfg := &flow.Config{
		Name: "get_product",
		From: &flow.FromConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"operation": "Query.product"},
		},
		To: &flow.ToConfig{
			Connector: "db",
			ConnectorParams: map[string]interface{}{
				"target": "products",
				"query":  "SELECT * FROM products WHERE sku = :sku AND category = :category",
			},
		},
		Returns: "Product",
	}

	got := argNames(inferArgsFromFlow(cfg, nil))
	if len(got) != 2 || got[0] != "category" || got[1] != "sku" {
		t.Errorf("the field publishes %v; the query names sku and category", got)
	}
}

func TestAMutationStillTakesItsTypedInput(t *testing.T) {
	// A mutation whose destination names its columns as placeholders would
	// otherwise publish one argument per column and lose the input object it
	// declares — which is what the federation example asks for.
	cfg := &flow.Config{
		Name: "create_product",
		From: &flow.FromConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"operation": "Mutation.createProduct"},
		},
		To: &flow.ToConfig{
			Connector: "db",
			ConnectorParams: map[string]interface{}{
				"target": "products",
				"query":  "INSERT INTO products (sku, name) VALUES (:sku, :name) RETURNING *",
			},
		},
		Returns: "Product",
	}

	got := argNames(inferArgsFromFlow(cfg, nil))
	if len(got) != 1 || got[0] != "input" {
		t.Errorf("the mutation publishes %v; it declares a typed input object", got)
	}
}

func TestAStepsParametersStillWin(t *testing.T) {
	// A mutation that gathers with steps takes those arguments, as before.
	cfg := &flow.Config{
		Name: "update",
		From: &flow.FromConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"operation": "Mutation.updatePrice"},
		},
		Steps: []*flow.StepConfig{{
			Name: "current", Connector: "db",
			ConnectorParams: map[string]interface{}{
				"query":  "SELECT * FROM products WHERE sku = :sku",
				"params": map[string]interface{}{"sku": "input.sku"},
			},
		}},
		Returns: "Product",
	}

	got := argNames(inferArgsFromFlow(cfg, nil))
	if len(got) != 1 || got[0] != "sku" {
		t.Errorf("the mutation publishes %v; its step names sku", got)
	}
}

func TestAnArgumentIsTypedByTheFieldItNames(t *testing.T) {
	// Everything was published as String, so `user(id: 1)` against an integer
	// column was refused with "Expected type String, found 1". Where the flow
	// says what it returns, the field of that name says what the argument is.
	cfg := &flow.Config{
		Name: "get_user",
		From: &flow.FromConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"operation": "Query.user"},
		},
		To: &flow.ToConfig{
			Connector: "db",
			ConnectorParams: map[string]interface{}{
				"query": "SELECT * FROM users WHERE id = :id AND email = :email",
			},
		},
		Returns: "User",
	}

	types := map[string]*validate.TypeSchema{
		"User": {Name: "User", Fields: []validate.FieldSchema{
			{Name: "id", Type: "number"},
			{Name: "email", Type: "string"},
		}},
	}

	got := argTypes(inferArgsFromFlow(cfg, types))
	if got["id"] != "number" {
		t.Errorf("id was published as %q; the type declares a number", got["id"])
	}
	if got["email"] != "string" {
		t.Errorf("email was published as %q", got["email"])
	}
}

func TestAnArgumentWithNoDeclaredFieldKeepsItsGuess(t *testing.T) {
	cfg := &flow.Config{
		Name: "search",
		From: &flow.FromConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"operation": "Query.search"},
		},
		To: &flow.ToConfig{
			Connector:       "db",
			ConnectorParams: map[string]interface{}{"query": "SELECT * FROM users WHERE name LIKE :term"},
		},
		Returns: "User",
	}

	types := map[string]*validate.TypeSchema{
		"User": {Name: "User", Fields: []validate.FieldSchema{{Name: "id", Type: "number"}}},
	}

	if got := argTypes(inferArgsFromFlow(cfg, types))["term"]; got != "string" {
		t.Errorf("an argument the type says nothing about was published as %q", got)
	}
}

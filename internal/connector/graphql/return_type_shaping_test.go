package graphql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

// A field is answered in the shape its declaration asks for.
//
// A flow answers with whatever its last stage produced: a transform produces
// a map, a step with one row is flattened to that row, a step with none is
// null. None of that is wrong for the flow, and all of it was wrong for a
// field declared as a list or as a scalar. A list field handed a map failed
// with `internal error`; a scalar field handed a map was stringified as
// `map[value:hello]`, HTTP 200, no error. The only configuration that served
// a list correctly was a bare step that happened to return two or more rows.
// Reproduced against 3.6.2 over the wire before the fix.

const shapedSDL = `
scalar JSON

enum Color { RED GREEN }

type Item { name: String }

type Query {
  list_tx:    [Item!]!
  list_step2: [Item!]!
  list_step1: [Item!]!
  list_step0: [Item!]!
  list_null:  [Item!]
  text_tx:    String!
  text_step:  String!
  count:      Int!
  ratio:      Float!
  ident:      ID!
  color:      Color!
  raw:        JSON!
  several:    String!
  maybe:      String
}
`

func TestAListFieldIsAnsweredWithAListWhateverTheFlowProduced(t *testing.T) {
	c, port := listeningServer(t, shapedSDL, false)

	rows := func(n int) []interface{} {
		out := make([]interface{}, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]interface{}{"name": fmt.Sprintf("r%d", i)})
		}
		return out
	}
	answer := func(v interface{}) HandlerFunc {
		return func(ctx context.Context, input map[string]interface{}) (interface{}, error) { return v, nil }
	}
	// transform { items = "as_list(step.rows).map(...)" }
	c.RegisterRoute("Query.list_tx", answer(map[string]interface{}{"items": rows(2)}))
	// a bare step: two rows stay a list, one row is flattened, none is null
	c.RegisterRoute("Query.list_step2", answer(rows(2)))
	c.RegisterRoute("Query.list_step1", answer(map[string]interface{}{"name": "only"}))
	c.RegisterRoute("Query.list_step0", answer(nil))
	c.RegisterRoute("Query.list_null", answer(nil))
	startAndWait(t, c, port)

	for field, want := range map[string]int{
		"list_tx":    2,
		"list_step2": 2,
		"list_step1": 1,
		"list_step0": 0,
		"list_null":  0,
	} {
		reply := askOverTheWire(t, port, fmt.Sprintf(`{ %s { name } }`, field))
		if errs, present := reply["errors"]; present {
			t.Errorf("%s: refused: %v", field, errs)
			continue
		}
		data, _ := reply["data"].(map[string]interface{})
		list, ok := data[field].([]interface{})
		if !ok || len(list) != want {
			t.Errorf("%s = %#v, want a list of %d", field, data[field], want)
			continue
		}
		if want > 0 {
			first, _ := list[0].(map[string]interface{})
			if name, _ := first["name"].(string); name == "" {
				t.Errorf("%s: the rows lost their fields: %#v", field, list[0])
			}
		}
	}
}

func TestAScalarFieldIsAnsweredWithTheValueNotTheMap(t *testing.T) {
	c, port := listeningServer(t, shapedSDL, false)

	answer := func(v interface{}) HandlerFunc {
		return func(ctx context.Context, input map[string]interface{}) (interface{}, error) { return v, nil }
	}
	c.RegisterRoute("Query.text_tx", answer(map[string]interface{}{"value": "hello"}))
	// one row, flattened by the step — the same map from the resolver's side
	c.RegisterRoute("Query.text_step", answer([]interface{}{map[string]interface{}{"value": "hello"}}))
	c.RegisterRoute("Query.count", answer(map[string]interface{}{"n": int64(3)}))
	c.RegisterRoute("Query.ratio", answer(map[string]interface{}{"r": 0.5}))
	c.RegisterRoute("Query.ident", answer(map[string]interface{}{"id": "u-1"}))
	c.RegisterRoute("Query.color", answer(map[string]interface{}{"color": "GREEN"}))
	// A JSON scalar is an object by definition: the map is the value.
	c.RegisterRoute("Query.raw", answer(map[string]interface{}{"a": 1, "b": "two"}))
	c.RegisterRoute("Query.maybe", answer(nil))
	startAndWait(t, c, port)

	reply := askOverTheWire(t, port, `{ text_tx text_step count ratio ident color raw maybe }`)
	if errs, present := reply["errors"]; present {
		t.Fatalf("refused: %v", errs)
	}
	data, _ := reply["data"].(map[string]interface{})
	for field, want := range map[string]interface{}{
		"text_tx":   "hello",
		"text_step": "hello",
		"count":     float64(3),
		"ratio":     0.5,
		"ident":     "u-1",
		"color":     "GREEN",
		"maybe":     nil,
	} {
		if got := data[field]; got != want {
			t.Errorf("%s = %#v, want %#v", field, got, want)
		}
	}
	raw, _ := data["raw"].(map[string]interface{})
	if raw["b"] != "two" {
		t.Errorf("a JSON scalar was reshaped: %#v", data["raw"])
	}
}

func TestAScalarFieldAnsweredWithSeveralValuesIsAnErrorNamingTheField(t *testing.T) {
	c, port := listeningServer(t, shapedSDL, false)
	c.RegisterRoute("Query.several", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"first": "a", "second": "b"}, nil
	})
	startAndWait(t, c, port)

	reply := askOverTheWire(t, port, `{ several }`)
	errs, _ := reply["errors"].([]interface{})
	if len(errs) == 0 {
		t.Fatalf("a two-field answer to a String! field was accepted: %#v", reply["data"])
	}
	msg := fmt.Sprintf("%v", errs[0])
	for _, want := range []string{"several", "String!", "first", "second"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q: %s", want, msg)
		}
	}
}

// The shaping is by the declared type, not by the shape of the answer, so an
// object field and a Boolean keep the answers they had.
func TestShapingLeavesObjectAndBooleanFieldsAlone(t *testing.T) {
	user := graphql.NewObject(graphql.ObjectConfig{
		Name:   "User",
		Fields: graphql.Fields{"id": &graphql.Field{Type: graphql.ID}},
	})
	for name, c := range map[string]struct {
		returns graphql.Output
		answer  interface{}
	}{
		"an object with one field": {graphql.NewNonNull(user), map[string]interface{}{"id": "u1"}},
		"a boolean with a count":   {graphql.NewNonNull(graphql.Boolean), map[string]interface{}{"affected": 1}},
		"a list already":           {graphql.NewList(user), []interface{}{map[string]interface{}{"id": "u1"}}},
		"a scalar already":         {graphql.String, "hello"},
		"a custom scalar's object": {JSONScalar, map[string]interface{}{"k": "v"}},
		"a list of scalars":        {graphql.NewList(graphql.String), []interface{}{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			p := graphql.ResolveParams{Info: graphql.ResolveInfo{FieldName: "f", ReturnType: c.returns}}
			got, err := shapeForReturnType(p, c.answer)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", c.answer) {
				t.Errorf("reshaped to %#v", got)
			}
		})
	}
}

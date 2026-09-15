package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
)

// A value the schema gives a default is a value the caller may leave out.
//
// The spec says so twice: an input field is required only when it is non-null
// AND has no default, and the same is true of an argument. The library
// validates both by looking at the type alone, so `loud: Boolean! = false`
// omitted was reported as a missing required value and the request failed
// before any flow ran. Everything short of a real call looked green:
// `mycel validate` is happy, the SDL is valid GraphQL, the flows are fine.
// Reported against 3.7.1 (#116).

const defaultsSDL = `
enum Volume { QUIET LOUD }

input Inner {
  depth: Int! = 1
}

input EchoInput {
  name:    String!
  loud:    Boolean! = false
  tries:   Int!     = 3
  ratio:   Float!   = 1.5
  volume:  Volume!  = QUIET
  tags:    [String!]! = ["a", "b"]
  inner:   Inner!   = { depth: 7 }
  tag:     String   = "none"
}

type EchoResult {
  value: String
}

input Filter {
  name:  String!
  exact: Boolean! = true
}

type Nested {
  again(loud: Boolean! = false): String
}

type Query {
  echo(input: EchoInput!): EchoResult!
  echoOptional(input: EchoInput): EchoResult!
  ping(loud: Boolean! = false, tries: Int! = 3): EchoResult!
  strict(input: StrictInput!): EchoResult!
  search(filters: [Filter!]!): EchoResult!
  nested: Nested
}

input StrictInput {
  name: String!
  loud: Boolean!
}
`

// askOverTheWireWith is askOverTheWire with variables, which is the other way
// a client sends an input object and the other place the default has to land.
func askOverTheWireWith(t *testing.T, port int, query string, variables map[string]interface{}) map[string]interface{} {
	t.Helper()

	body, _ := json.Marshal(map[string]interface{}{"query": query, "variables": variables})
	req, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/graphql", port), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	defer resp.Body.Close()

	var answer map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	return answer
}

// defaultsServer stands up the schema above with a flow that reports back the
// input it was handed, so a test can assert both that the call was accepted
// and what the default actually became.
func defaultsServer(t *testing.T) (int, func() map[string]interface{}) {
	t.Helper()

	c, port := listeningServer(t, defaultsSDL, false)

	seen := make(chan map[string]interface{}, 8)
	answer := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		seen <- input
		return map[string]interface{}{"value": "ok"}, nil
	}
	c.RegisterRoute("Query.echo", answer)
	c.RegisterRoute("Query.echoOptional", answer)
	c.RegisterRoute("Query.ping", answer)
	c.RegisterRoute("Query.strict", answer)
	c.RegisterRoute("Query.search", answer)
	c.RegisterRoute("Query.nested", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"again": "ok"}, nil
	})
	c.RegisterRoute("Nested.again", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		seen <- input
		return "ok", nil
	})
	startAndWait(t, c, port)

	return port, func() map[string]interface{} {
		select {
		case in := <-seen:
			return in
		default:
			t.Fatal("no flow ran")
			return nil
		}
	}
}

func refused(t *testing.T, reply map[string]interface{}) string {
	t.Helper()
	errs, present := reply["errors"]
	if !present {
		return ""
	}
	return fmt.Sprintf("%v", errs)
}

func TestAnInputFieldWithADefaultCanBeOmitted(t *testing.T) {
	port, lastInput := defaultsServer(t)

	reply := askOverTheWire(t, port, `{ echo(input: {name: "hello"}) { value } }`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused a query that omits fields the schema gives defaults: %s", msg)
	}

	in := lastInput()
	for field, want := range map[string]interface{}{
		"loud":   false,
		"tries":  3,
		"ratio":  1.5,
		"volume": "QUIET",
		"tag":    "none",
	} {
		if got := in[field]; got != want {
			t.Errorf("input.%s = %#v, want %#v", field, got, want)
		}
	}
	if got, ok := in["tags"].([]interface{}); !ok || len(got) != 2 || got[0] != "a" {
		t.Errorf("input.tags = %#v, want the declared [a b]", in["tags"])
	}
	if inner, ok := in["inner"].(map[string]interface{}); !ok || inner["depth"] != 7 {
		t.Errorf("input.inner = %#v, want {depth: 7}", in["inner"])
	}
}

func TestAnInputFieldWithADefaultCanBeOmittedInAVariable(t *testing.T) {
	port, lastInput := defaultsServer(t)

	reply := askOverTheWireWith(t, port,
		`query E($in: EchoInput!) { echo(input: $in) { value } }`,
		map[string]interface{}{"in": map[string]interface{}{"name": "hello"}})
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused variables that omit fields the schema gives defaults: %s", msg)
	}

	in := lastInput()
	if in["loud"] != false || in["tries"] != 3 {
		t.Errorf("defaults did not reach the flow: loud=%#v tries=%#v", in["loud"], in["tries"])
	}
}

func TestAnArgumentWithADefaultCanBeOmitted(t *testing.T) {
	// The same spec rule, the other place it is written: a required argument is
	// one that is non-null and has no default.
	port, lastInput := defaultsServer(t)

	reply := askOverTheWire(t, port, `{ ping { value } }`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused a field whose arguments all have defaults: %s", msg)
	}

	in := lastInput()
	if in["loud"] != false || in["tries"] != 3 {
		t.Errorf("argument defaults did not reach the flow: loud=%#v tries=%#v", in["loud"], in["tries"])
	}
}

func TestAValueGivenByTheCallerWinsOverTheDefault(t *testing.T) {
	port, lastInput := defaultsServer(t)

	reply := askOverTheWire(t, port, `{ echo(input: {name: "hello", loud: true, tries: 9}) { value } }`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused: %s", msg)
	}
	in := lastInput()
	if in["loud"] != true || in["tries"] != 9 {
		t.Errorf("caller's values were overwritten: loud=%#v tries=%#v", in["loud"], in["tries"])
	}

	reply = askOverTheWireWith(t, port,
		`query E($in: EchoInput!) { echo(input: $in) { value } }`,
		map[string]interface{}{"in": map[string]interface{}{"name": "hello", "loud": true, "tries": 9}})
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused: %s", msg)
	}
	in = lastInput()
	if in["loud"] != true || in["tries"] != 9 {
		t.Errorf("caller's variables were overwritten: loud=%#v tries=%#v", in["loud"], in["tries"])
	}
}

func TestAFieldWithNoDefaultIsStillRequired(t *testing.T) {
	// The fix must not turn validation off: non-null with no default is the one
	// shape that is genuinely required.
	port, _ := defaultsServer(t)

	if msg := refused(t, askOverTheWire(t, port, `{ strict(input: {name: "hello"}) { value } }`)); msg == "" {
		t.Error("a non-null field with no default was accepted when omitted")
	}
	if msg := refused(t, askOverTheWireWith(t, port,
		`query S($in: StrictInput!) { strict(input: $in) { value } }`,
		map[string]interface{}{"in": map[string]interface{}{"name": "hello"}})); msg == "" {
		t.Error("a non-null field with no default was accepted when omitted from variables")
	}
}

func TestADefaultInsideAListOfInputObjectsIsFilledIn(t *testing.T) {
	port, lastInput := defaultsServer(t)

	reply := askOverTheWire(t, port, `{ search(filters: [{name: "a"}, {name: "b", exact: false}]) { value } }`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused a list of input objects with omitted defaults: %s", msg)
	}

	filters, ok := lastInput()["filters"].([]interface{})
	if !ok || len(filters) != 2 {
		t.Fatalf("filters = %#v, want two", lastInput()["filters"])
	}
	first, _ := filters[0].(map[string]interface{})
	second, _ := filters[1].(map[string]interface{})
	if first["exact"] != true {
		t.Errorf("the omitted default did not reach the first filter: %#v", first)
	}
	if second["exact"] != false {
		t.Errorf("the second filter's own value was overwritten: %#v", second)
	}
}

func TestADefaultedArgumentOnANestedFieldIsFilledIn(t *testing.T) {
	// The walk has to follow the query into the types it selects, not only the
	// root: `nested { again }` names a field with its own defaulted argument.
	port, _ := defaultsServer(t)

	reply := askOverTheWire(t, port, `{ nested { again } }`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused a nested field whose argument has a default: %s", msg)
	}
}

func TestADefaultIsFilledInThroughAFragment(t *testing.T) {
	// A fragment is where a hand-written walk over the query stops knowing what
	// type it is looking at.
	port, _ := defaultsServer(t)

	reply := askOverTheWire(t, port, `
		{ nested { ...bits } }
		fragment bits on Nested { again }
	`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused a defaulted argument inside a fragment: %s", msg)
	}
}

func TestOnlyTheNamedOperationsVariablesAreFilledIn(t *testing.T) {
	port, lastInput := defaultsServer(t)

	reply := askOverTheWireWith(t, port, `
		query A($in: EchoInput!) { echo(input: $in) { value } }
		query B($in: StrictInput!) { strict(input: $in) { value } }
	`, map[string]interface{}{"in": map[string]interface{}{"name": "hello"}})
	// Without an operationName the request names no operation to run, so this
	// only has to be refused for that reason rather than crash.
	if msg := refused(t, reply); msg == "" {
		t.Error("a request naming neither operation was answered")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query": `query A($in: EchoInput!) { echo(input: $in) { value } }
			query B($in: StrictInput!) { strict(input: $in) { value } }`,
		"variables":     map[string]interface{}{"in": map[string]interface{}{"name": "hello"}},
		"operationName": "A",
	})
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/graphql", port), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if msg := refused(t, answer); msg != "" {
		t.Fatalf("refused the named operation: %s", msg)
	}
	if in := lastInput(); in["loud"] != false || in["tries"] != 3 {
		t.Errorf("the named operation's variables were not filled in: %#v", in)
	}
}

func TestTheRewrittenRequestIsStillTheSameRequest(t *testing.T) {
	// Filling a default in means writing the document back out, so everything
	// the caller wrote — an alias, a fragment, a directive, a variable — has to
	// survive the round trip.
	port, lastInput := defaultsServer(t)

	reply := askOverTheWireWith(t, port, `
		query E($name: String!, $skip: Boolean!) {
			mine: echo(input: {name: $name}) {
				...bits @skip(if: $skip)
			}
		}
		fragment bits on EchoResult { value }
	`, map[string]interface{}{"name": "hello", "skip": false})
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused after the rewrite: %s", msg)
	}

	data, _ := reply["data"].(map[string]interface{})
	mine, ok := data["mine"].(map[string]interface{})
	if !ok || mine["value"] != "ok" {
		t.Errorf("the alias or the fragment did not survive: %#v", data)
	}
	if in := lastInput(); in["name"] != "hello" || in["loud"] != false {
		t.Errorf("the variable or the default was lost: %#v", in)
	}
}

func TestADefaultTheSchemaCannotRenderIsLeftAlone(t *testing.T) {
	// astFromDefault refuses what it cannot write as a literal — a custom
	// scalar holding an object, say. That has to leave the request as it was
	// rather than produce a broken one.
	if _, ok := astFromDefault(map[string]interface{}{"a": 1}, JSONScalar); ok {
		t.Error("an unrenderable default was rendered anyway")
	}
	if _, ok := astFromDefault(nil, graphql.String); ok {
		t.Error("a nil default was rendered")
	}
}

func TestASubscriptionMayOmitADefaultedArgument(t *testing.T) {
	// A subscription is validated the same way and was refused the same way,
	// except that a WebSocket client sees the refusal as an error message on
	// its subscription rather than as an HTTP response.
	sdl := `
type Order { id: ID! label(loud: Boolean! = false): String }
type Query { ping: String }
type Subscription { orderPlaced: Order }
`
	path := filepath.Join(t.TempDir(), "schema.graphql")
	if err := os.WriteFile(path, []byte(sdl), 0o644); err != nil {
		t.Fatal(err)
	}

	builder := NewSchemaBuilder()
	if err := builder.LoadSDL(path); err != nil {
		t.Fatal(err)
	}
	pass := func(_ context.Context, input map[string]interface{}) (interface{}, error) { return input, nil }
	if err := builder.RegisterHandlerWithReturnType("Subscription.orderPlaced", pass, "Order"); err != nil {
		t.Fatal(err)
	}
	schema, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	manager := NewSubscriptionManager(schema, slog.New(slog.NewTextHandler(io.Discard, nil)))
	manager.pubsub = builder.pubsub
	server := httptest.NewServer(manager.Handler())
	t.Cleanup(server.Close)

	client := connect(t, "ws"+strings.TrimPrefix(server.URL, "http"))
	client.say(map[string]interface{}{"type": msgConnectionInit})
	if ack := client.hear(); ack["type"] != msgConnectionAck {
		t.Fatalf("expected an acknowledgement, got %v", ack)
	}
	client.say(map[string]interface{}{
		"id":      "1",
		"type":    msgSubscribe,
		"payload": map[string]interface{}{"query": `subscription { orderPlaced { label } }`},
	})

	// Give the subscription a moment to attach before publishing, or the event
	// is sent to nobody — a refusal, if it comes, arrives before it.
	time.Sleep(150 * time.Millisecond)
	builder.pubsub.Publish("orderPlaced", map[string]interface{}{"id": "7", "label": "hi"})

	message := client.hear()
	if message["type"] == msgError {
		t.Fatalf("the subscription was refused for omitting an argument with a default: %v", message["payload"])
	}
	if message["type"] != msgNext {
		t.Fatalf("expected an event, got %v", message)
	}
}

func TestANumericDefaultIsANumber(t *testing.T) {
	// The AST keeps a written number as the text of it, and handing that on
	// made `tries: Int! = 3` reach a flow as the string "3" — and introspection
	// report the default quoted, which is not the same SDL.
	parsed, err := ParseSDLComplete(`
input Paging {
  limit: Int!    = 25
  ratio: Float!  = 1.5
  label: String! = "all"
}
type Query { page(limit: Int! = 10): String }
`)
	if err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}

	fields := parsed.Inputs["Paging"].Fields
	if got := fields["limit"].DefaultValue; got != 25 {
		t.Errorf("limit default = %#v, want the number 25", got)
	}
	if got := fields["ratio"].DefaultValue; got != 1.5 {
		t.Errorf("ratio default = %#v, want the number 1.5", got)
	}
	if got := fields["label"].DefaultValue; got != "all" {
		t.Errorf("label default = %#v, want the string", got)
	}
	if got := parsed.Query.Fields["page"].Args["limit"].DefaultValue; got != 10 {
		t.Errorf("argument default = %#v, want the number 10", got)
	}
}

func TestIntrospectionReportsTheDefaultAsWritten(t *testing.T) {
	// A client reads the contract here, and a default reported as `"3"` is a
	// different contract from `3`.
	port, _ := defaultsServer(t)

	reply := askOverTheWire(t, port, `{ __type(name: "EchoInput") { inputFields { name defaultValue } } }`)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused: %s", msg)
	}

	data, _ := reply["data"].(map[string]interface{})
	ttype, _ := data["__type"].(map[string]interface{})
	fields, _ := ttype["inputFields"].([]interface{})
	written := map[string]interface{}{}
	for _, field := range fields {
		f, _ := field.(map[string]interface{})
		written[fmt.Sprintf("%v", f["name"])] = f["defaultValue"]
	}
	for name, want := range map[string]interface{}{
		"tries": "3",
		"ratio": "1.5",
		"loud":  "false",
		"tag":   `"none"`,
	} {
		if got := written[name]; got != want {
			t.Errorf("%s reports its default as %#v, want %#v", name, got, want)
		}
	}
}

func TestADefaultWrittenOnAVariableIsFilledInToo(t *testing.T) {
	// A variable can carry its own default object, and that object is checked
	// by the same rule — so it was refused for the same reason.
	port, lastInput := defaultsServer(t)

	reply := askOverTheWireWith(t, port,
		`query E($in: EchoInput = {name: "written"}) { echoOptional(input: $in) { value } }`,
		nil)
	if msg := refused(t, reply); msg != "" {
		t.Fatalf("refused a variable whose own default omits defaulted fields: %s", msg)
	}
	if in := lastInput(); in["name"] != "written" || in["tries"] != 3 {
		t.Errorf("the variable's default did not reach the flow: %#v", in)
	}
}

func TestASchemaWithNoSuchDefaultIsLeftAlone(t *testing.T) {
	// The rewrite costs a parse and a print, so a schema that declares no
	// omissible default — which is most of them, and includes the
	// introspection types every schema carries — must not pay for it.
	c, _ := listeningServer(t, servedUsersSDL, false)
	schema, err := c.schemaBuilder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if newDefaultFiller(schema).needed {
		t.Error("a schema with no non-null default was marked as needing the rewrite")
	}

	query := `{ users { id } }`
	got, _ := newDefaultFiller(schema).apply(query, nil, "")
	if got != query {
		t.Errorf("the query was rewritten anyway: %q", got)
	}
}

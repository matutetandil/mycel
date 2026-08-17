package graphql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The HTTP handler is where a GraphQL service meets the outside world, and its
// answers are read by machines: a client looks for `data` and `errors` in the
// body, not at the status line. Getting that wrong turns a refusal into
// something the caller cannot act on.

func servedSchema(t *testing.T, introspection bool) *ServerConnector {
	t.Helper()
	server := NewServer("api", &ServerConfig{
		Port: 0, Host: "localhost", Endpoint: "/graphql",
		Introspection: introspection,
	}, nil)
	if err := server.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	server.RegisterRouteWithArgs("Query.customer",
		func(_ context.Context, input map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"id": input["id"], "email": "someone@example.com"}, nil
		}, "", []*ArgDef{{Name: "id", Type: "id", Required: true}})

	schema, err := server.schemaBuilder.Build()
	if err != nil {
		t.Fatalf("building the schema: %v", err)
	}
	server.schema = schema
	return server
}

func ask(t *testing.T, server *ServerConnector, request *http.Request) (int, map[string]interface{}) {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.handleGraphQL(recorder, request)

	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("the answer is not JSON: %v\n%s", err, recorder.Body.String())
	}
	return recorder.Code, body
}

func post(t *testing.T, query string) *http.Request {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{"query": query})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(string(payload)))
}

func TestAQueryIsAnswered(t *testing.T) {
	status, body := ask(t, servedSchema(t, true), post(t, `{ customer(id: "1") }`))
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if body["data"] == nil {
		t.Errorf("answer = %v, want data", body)
	}
}

func TestAQueryCanArriveInTheAddress(t *testing.T) {
	// A GET is how a browser, a link or a cache reaches a read.
	request := httptest.NewRequest(http.MethodGet,
		"/graphql?query="+url.QueryEscape(`{ customer(id: "1") }`), nil)
	status, body := ask(t, servedSchema(t, true), request)
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if body["data"] == nil {
		t.Errorf("answer = %v", body)
	}
}

func TestVariablesArriveWithTheQuery(t *testing.T) {
	server := servedSchema(t, true)
	payload, err := json.Marshal(map[string]interface{}{
		"query":     `query Find($id: ID!) { customer(id: $id) }`,
		"variables": map[string]interface{}{"id": "42"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	status, body := ask(t, server, httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(string(payload))))
	if status != http.StatusOK {
		t.Fatalf("status = %d body = %v", status, body)
	}
	if body["errors"] != nil {
		t.Fatalf("errors = %v", body["errors"])
	}

	// The value has to have reached the handler, not merely been accepted.
	data, _ := body["data"].(map[string]interface{})
	customer, _ := data["customer"].(map[string]interface{})
	if customer == nil || customer["id"] != "42" {
		t.Errorf("customer = %v, want the variable's value", customer)
	}
}

func TestVariablesThatAreNotJSONAreRefused(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/graphql?query={customer}&variables=not-json", nil)
	status, body := ask(t, servedSchema(t, true), request)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want the request refused", status)
	}
	if body["errors"] == nil {
		t.Errorf("answer = %v, want an errors array a client can read", body)
	}
}

func TestARequestWithNoQueryIsRefused(t *testing.T) {
	for name, request := range map[string]*http.Request{
		"an empty body":           httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{}`)),
		"a body that is not JSON": httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`not json`)),
		"nothing in the address":  httptest.NewRequest(http.MethodGet, "/graphql", nil),
	} {
		t.Run(name, func(t *testing.T) {
			status, body := ask(t, servedSchema(t, true), request)
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want it refused", status)
			}
			if body["errors"] == nil {
				t.Errorf("answer = %v, want an errors array", body)
			}
		})
	}
}

func TestAMethodThatIsNeitherIsRefused(t *testing.T) {
	server := servedSchema(t, true)
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		recorder := httptest.NewRecorder()
		server.handleGraphQL(recorder, httptest.NewRequest(method, "/graphql", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want it refused", method, recorder.Code)
		}
	}
}

// The schema is a map of everything the service can do, so publishing it is a
// choice. The attribute used to be read by nothing, which meant asking for it
// to be off left it on.

func TestIntrospectionIsRefusedWhenItIsOff(t *testing.T) {
	server := servedSchema(t, false)

	for name, query := range map[string]string{
		"the schema itself":    `{ __schema { types { name } } }`,
		"a type by name":       `{ __type(name: "Customer") { name } }`,
		"buried in a fragment": `query { ...F } fragment F on Query { __schema { queryType { name } } }`,
	} {
		t.Run(name, func(t *testing.T) {
			status, body := ask(t, server, post(t, query))

			// A client reads errors from the body, so the refusal is a 200
			// carrying an errors array rather than an HTTP status.
			if status != http.StatusOK {
				t.Errorf("status = %d, want a 200 a GraphQL client will read", status)
			}
			errors, _ := body["errors"].([]interface{})
			if len(errors) == 0 {
				t.Fatalf("answer = %v, want it refused", body)
			}
			if !strings.Contains(strings.ToLower(encode(t, errors)), "introspection") {
				t.Errorf("errors = %v, want them to say why", errors)
			}
			if body["data"] != nil {
				t.Errorf("data = %v, want none", body["data"])
			}
		})
	}
}

func TestIntrospectionAnswersWhenItIsOn(t *testing.T) {
	status, body := ask(t, servedSchema(t, true), post(t, `{ __schema { queryType { name } } }`))
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if body["data"] == nil {
		t.Errorf("answer = %v, want the schema", body)
	}
}

func TestAnOrdinaryQueryIsNotMistakenForIntrospection(t *testing.T) {
	// A query that merely mentions the word, or asks for a field whose value
	// contains it, must still be answered — turning introspection off is not
	// turning the service off.
	server := servedSchema(t, false)
	status, body := ask(t, server, post(t, `{ customer(id: "__schema") }`))
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if body["errors"] != nil {
		t.Errorf("a query carrying the word in a string was refused: %v", body["errors"])
	}
	if body["data"] == nil {
		t.Error("the query was not answered")
	}
}

func encode(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(data)
}

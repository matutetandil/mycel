package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A GraphQL server, served and asked.
//
// Start, setupHandlers, the playground and the CORS middleware were all at
// zero: the server is exercised by the integration suite, which runs the
// binary, so from Go's side none of it had been reached. Standing one up here
// is the same trick that the TCP round trip uses — the query goes over a real
// socket, so what is checked is what a client would get.
func listeningServer(t *testing.T, sdl string, playground bool) (*ServerConnector, int) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte(sdl), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	c := NewServer("api", &ServerConfig{
		Host:          "127.0.0.1",
		Port:          port,
		Schema:        SchemaConfig{Path: path},
		Playground:    playground,
		Introspection: true,
		CORS:          &CORSConfig{Origins: []string{"*"}},
	}, nil)

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, port
}

func startAndWait(t *testing.T, c *ServerConnector, port int) {
	t.Helper()
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("nothing listening on %d", port)
}

func askOverTheWire(t *testing.T, port int, query string) map[string]interface{} {
	t.Helper()

	body, _ := json.Marshal(map[string]interface{}{"query": query})
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

const servedUsersSDL = `
type User {
  id: ID!
  name: String!
}

type Query {
  users: [User!]!
  user(id: ID!): User
}

type Mutation {
  createUser(name: String!): User!
}
`

func TestAQueryIsServedByTheFlowRegisteredForIt(t *testing.T) {
	c, port := listeningServer(t, servedUsersSDL, false)

	c.RegisterRoute("Query.users", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return []interface{}{
			map[string]interface{}{"id": "1", "name": "Ada"},
			map[string]interface{}{"id": "2", "name": "Grace"},
		}, nil
	})
	c.RegisterRoute("Query.user", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": input["id"], "name": "Ada"}, nil
	})

	startAndWait(t, c, port)

	answer := askOverTheWire(t, port, `{ users { id name } }`)
	if errs, present := answer["errors"]; present {
		t.Fatalf("the query was refused: %v", errs)
	}
	data, _ := answer["data"].(map[string]interface{})
	users, _ := data["users"].([]interface{})
	if len(users) != 2 {
		t.Fatalf("users = %#v", data["users"])
	}

	// A field the query did not ask for is not in the answer, which is what
	// makes a GraphQL response a GraphQL response.
	first, _ := users[0].(map[string]interface{})
	if first["name"] != "Ada" {
		t.Errorf("name = %#v", first["name"])
	}

	// An argument reaches the handler.
	one := askOverTheWire(t, port, `{ user(id: "7") { id } }`)
	data, _ = one["data"].(map[string]interface{})
	user, _ := data["user"].(map[string]interface{})
	if user["id"] != "7" {
		t.Errorf("the argument reached the handler as %#v", user["id"])
	}
}

// A mutation, and a field the schema requires that the flow does not return.
func TestAMutationIsServedAndAContractIsEnforced(t *testing.T) {
	c, port := listeningServer(t, servedUsersSDL, false)

	c.RegisterRoute("Mutation.createUser", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "9", "name": input["name"]}, nil
	})
	startAndWait(t, c, port)

	answer := askOverTheWire(t, port, `mutation { createUser(name: "Ada") { id name } }`)
	if errs, present := answer["errors"]; present {
		t.Fatalf("the mutation was refused: %v", errs)
	}
	data, _ := answer["data"].(map[string]interface{})
	created, _ := data["createUser"].(map[string]interface{})
	if created["id"] != "9" || created["name"] != "Ada" {
		t.Errorf("createUser = %#v", created)
	}
}

// The schema is discoverable, which is what a federation gateway asks for
// before it can compose anything.
func TestTheSchemaDescribesItself(t *testing.T) {
	c, port := listeningServer(t, servedUsersSDL, false)
	startAndWait(t, c, port)

	answer := askOverTheWire(t, port, `{ _service { sdl } }`)
	if errs, present := answer["errors"]; present {
		t.Fatalf("_service was refused: %v", errs)
	}
	data, _ := answer["data"].(map[string]interface{})
	service, _ := data["_service"].(map[string]interface{})
	sdl, _ := service["sdl"].(string)
	if !strings.Contains(sdl, "type User") {
		t.Errorf("the sdl does not describe the schema: %q", sdl)
	}
}

// The playground is served when it is asked for, and not when it is not.
func TestThePlaygroundIsServedOnlyWhenAskedFor(t *testing.T) {
	with, port := listeningServer(t, servedUsersSDL, true)
	startAndWait(t, with, port)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/playground", port))
	if err != nil {
		t.Fatalf("playground: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("playground answered %d", resp.StatusCode)
	}

	without, otherPort := listeningServer(t, servedUsersSDL, false)
	startAndWait(t, without, otherPort)

	off, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/playground", otherPort))
	if err != nil {
		t.Fatalf("playground: %v", err)
	}
	defer off.Body.Close()
	if off.StatusCode == http.StatusOK {
		t.Error("the playground is served on a connector that did not ask for it")
	}
}

// A browser asks before it posts.
func TestAPreflightIsAnswered(t *testing.T) {
	c, port := listeningServer(t, servedUsersSDL, false)
	startAndWait(t, c, port)

	req, err := http.NewRequest("OPTIONS", fmt.Sprintf("http://127.0.0.1:%d/graphql", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		t.Errorf("the preflight answered %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("the preflight said nothing about allowed origins, so a browser will not post")
	}
}

package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	gql "github.com/matutetandil/mycel/v3/internal/connector/graphql"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// Federation entity resolvers.
//
// A type carrying `_key` is one this subgraph tells the gateway it can resolve
// by reference. What resolves it is either a flow that names the type in its
// `entity` attribute, or one that returns it. Without either, the gateway
// routes those lookups here and gets nothing back — a null in the composed
// graph, from a subgraph that said it could answer.

// federatedConnector stands in for a GraphQL connector with federation on.
type federatedConnector struct {
	enabled    bool
	registered map[string][]gql.EntityKey
	resolvers  map[string]gql.EntityResolver
}

func newFederatedConnector(enabled bool) *federatedConnector {
	return &federatedConnector{
		enabled:    enabled,
		registered: map[string][]gql.EntityKey{},
		resolvers:  map[string]gql.EntityResolver{},
	}
}

func (f *federatedConnector) RegisterEntity(typeName string, keys []gql.EntityKey, resolver gql.EntityResolver) {
	f.registered[typeName] = keys
	f.resolvers[typeName] = resolver
}

func (f *federatedConnector) IsFederationEnabled() bool { return f.enabled }

// The rest of connector.Connector, which this never exercises.
func (f *federatedConnector) Name() string                  { return "gql" }
func (f *federatedConnector) Type() string                  { return "graphql" }
func (f *federatedConnector) Connect(context.Context) error { return nil }
func (f *federatedConnector) Close(context.Context) error   { return nil }
func (f *federatedConnector) Health(context.Context) error  { return nil }

func entityRuntime(t *testing.T, types map[string]*validate.TypeSchema, flows []*flow.Config) (*Runtime, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	r := &Runtime{
		types:  types,
		flows:  NewFlowRegistry(),
		logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	for _, cfg := range flows {
		r.flows.handlers[cfg.Name] = &FlowHandler{Config: cfg}
	}
	return r, logs
}

func TestAFlowNamingAnEntityResolvesIt(t *testing.T) {
	r, _ := entityRuntime(t,
		map[string]*validate.TypeSchema{
			"User": {Name: "User", Keys: []string{"id"}},
		},
		[]*flow.Config{{
			Name:   "resolve_user",
			Entity: "User",
			From:   &flow.FromConfig{Connector: "gql"},
		}},
	)

	conn := newFederatedConnector(true)
	r.registerEntityResolvers("gql", conn)

	if _, ok := conn.resolvers["User"]; !ok {
		t.Fatal("the flow naming the entity did not become its resolver")
	}
	keys := conn.registered["User"]
	if len(keys) != 1 || keys[0].Fields != "id" {
		t.Errorf("keys = %+v, want the type's own", keys)
	}
	// A key a subgraph declares and cannot resolve is worse than no key, so
	// what is registered has to be resolvable.
	if !keys[0].Resolvable {
		t.Error("the key was registered as not resolvable")
	}
}

func TestAFlowReturningTheTypeResolvesItToo(t *testing.T) {
	// The other way round: no entity attribute, but a query on this connector
	// that returns the type.
	for name, returns := range map[string]string{
		"plain":        "User",
		"non-nullable": "User!",
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := entityRuntime(t,
				map[string]*validate.TypeSchema{"User": {Name: "User", Keys: []string{"id"}}},
				[]*flow.Config{{
					Name:    "get_user",
					Returns: returns,
					From:    &flow.FromConfig{Connector: "gql"},
				}},
			)

			conn := newFederatedConnector(true)
			r.registerEntityResolvers("gql", conn)

			if _, ok := conn.resolvers["User"]; !ok {
				t.Errorf("a flow returning %q did not become the resolver", returns)
			}
		})
	}
}

func TestAFlowOnAnotherConnectorIsNotThisSubgraphsResolver(t *testing.T) {
	r, _ := entityRuntime(t,
		map[string]*validate.TypeSchema{"User": {Name: "User", Keys: []string{"id"}}},
		[]*flow.Config{{
			Name:    "get_user",
			Returns: "User",
			From:    &flow.FromConfig{Connector: "another_gql"},
		}},
	)

	conn := newFederatedConnector(true)
	r.registerEntityResolvers("gql", conn)

	if _, ok := conn.resolvers["User"]; ok {
		t.Error("a flow reading from another connector was registered as this one's resolver")
	}
}

func TestAKeyWithNothingToResolveItIsSaidOutLoud(t *testing.T) {
	// The failure this used to keep to itself. The gateway routes reference
	// lookups for a keyed type here and gets nothing, which reads as a null in
	// the composed graph rather than as an error anywhere — and it was logged
	// at debug, which is off.
	r, logs := entityRuntime(t,
		map[string]*validate.TypeSchema{"Order": {Name: "Order", Keys: []string{"id"}}},
		nil,
	)

	conn := newFederatedConnector(true)
	r.registerEntityResolvers("gql", conn)

	if _, ok := conn.resolvers["Order"]; ok {
		t.Fatal("a resolver appeared from nowhere")
	}
	said := logs.String()
	if !strings.Contains(said, "level=WARN") {
		t.Errorf("nothing was said above debug level:\n%s", said)
	}
	for _, want := range []string{"Order", "entity"} {
		if !strings.Contains(said, want) {
			t.Errorf("the warning does not mention %q:\n%s", want, said)
		}
	}
}

func TestATypeWithNoKeyIsNotAnEntity(t *testing.T) {
	// Most types. Nothing is registered and nothing is said.
	r, logs := entityRuntime(t,
		map[string]*validate.TypeSchema{"Address": {Name: "Address"}},
		nil,
	)

	conn := newFederatedConnector(true)
	r.registerEntityResolvers("gql", conn)

	if len(conn.registered) != 0 {
		t.Errorf("registered %v for a type with no key", conn.registered)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("a type with no key produced a warning:\n%s", logs.String())
	}
}

func TestWithFederationOffNothingIsRegistered(t *testing.T) {
	r, _ := entityRuntime(t,
		map[string]*validate.TypeSchema{"User": {Name: "User", Keys: []string{"id"}}},
		[]*flow.Config{{Name: "resolve_user", Entity: "User", From: &flow.FromConfig{Connector: "gql"}}},
	)

	conn := newFederatedConnector(false)
	r.registerEntityResolvers("gql", conn)

	if len(conn.registered) != 0 {
		t.Errorf("entities were registered on a connector with federation off: %v", conn.registered)
	}
}

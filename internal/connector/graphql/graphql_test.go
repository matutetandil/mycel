package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// TestSchemaBuilder tests schema construction.
func TestSchemaBuilder(t *testing.T) {
	builder := NewSchemaBuilder()

	// Register a query handler
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return []map[string]interface{}{
			{"id": 1, "name": "John"},
			{"id": 2, "name": "Jane"},
		}, nil
	}

	if err := builder.RegisterHandler("Query.users", handler); err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Register a mutation handler
	mutationHandler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"id":   1,
			"name": input["name"],
		}, nil
	}

	if err := builder.RegisterHandler("Mutation.createUser", mutationHandler); err != nil {
		t.Fatalf("failed to register mutation handler: %v", err)
	}

	// Build schema
	schema, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	if schema == nil {
		t.Fatal("schema should not be nil")
	}
}

// TestServerConnector tests the GraphQL server connector.
func TestServerConnector(t *testing.T) {
	config := &ServerConfig{
		Port:       0, // Use random port
		Host:       "localhost",
		Endpoint:   "/graphql",
		Playground: true,
	}

	server := NewServer("test-graphql", config, nil)

	if server.Name() != "test-graphql" {
		t.Errorf("expected name 'test-graphql', got '%s'", server.Name())
	}

	if server.Type() != "graphql" {
		t.Errorf("expected type 'graphql', got '%s'", server.Type())
	}

	// Connect
	ctx := context.Background()
	if err := server.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Register a handler
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"message": "Hello"}, nil
	}
	server.RegisterRoute("Query.hello", handler)

	// Health check
	if err := server.Health(ctx); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	// Close
	if err := server.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestClientConnector tests the GraphQL client connector.
func TestClientConnector(t *testing.T) {
	// Create a mock GraphQL server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Return mock response
		response := GraphQLResponse{
			Data: map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{"id": 1, "name": "John"},
					map[string]interface{}{"id": 2, "name": "Jane"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &ClientConfig{
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
	}

	client := NewClient("test-client", config)

	if client.Name() != "test-client" {
		t.Errorf("expected name 'test-client', got '%s'", client.Name())
	}

	if client.Type() != "graphql" {
		t.Errorf("expected type 'graphql', got '%s'", client.Type())
	}

	// Connect
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Read (query)
	query := connector.Query{
		Target: "{ users { id name } }",
	}
	result, err := client.Read(ctx, query)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(result.Rows) == 0 {
		t.Fatal("expected at least one row")
	}

	// Close
	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestClientAuthBearer tests Bearer token authentication.
func TestClientAuthBearer(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		response := GraphQLResponse{Data: map[string]interface{}{"test": true}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &ClientConfig{
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
		Auth: &AuthConfig{
			Type:  "bearer",
			Token: "test-token-123",
		},
	}

	client := NewClient("test-auth", config)
	ctx := context.Background()
	client.Connect(ctx)

	client.Read(ctx, connector.Query{Target: "{ test }"})

	expected := "Bearer test-token-123"
	if receivedAuth != expected {
		t.Errorf("expected auth '%s', got '%s'", expected, receivedAuth)
	}
}

// TestClientAuthAPIKey tests API key authentication.
func TestClientAuthAPIKey(t *testing.T) {
	var receivedAPIKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")

		response := GraphQLResponse{Data: map[string]interface{}{"test": true}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &ClientConfig{
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
		Auth: &AuthConfig{
			Type:   "apikey",
			APIKey: "my-api-key",
		},
	}

	client := NewClient("test-apikey", config)
	ctx := context.Background()
	client.Connect(ctx)

	client.Read(ctx, connector.Query{Target: "{ test }"})

	if receivedAPIKey != "my-api-key" {
		t.Errorf("expected api key 'my-api-key', got '%s'", receivedAPIKey)
	}
}

// TestFactory tests the GraphQL factory.
func TestFactory(t *testing.T) {
	factory := NewFactory(nil)

	// Test Supports
	if !factory.Supports("graphql", "") {
		t.Error("factory should support type 'graphql'")
	}

	if factory.Supports("rest", "") {
		t.Error("factory should not support type 'rest'")
	}

	// Test creating server
	ctx := context.Background()
	serverCfg := &connector.Config{
		Name:   "test-server",
		Type:   "graphql",
		Driver: "server",
		Properties: map[string]interface{}{
			"port":       4000,
			"playground": true,
		},
	}

	conn, err := factory.Create(ctx, serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if _, ok := conn.(*ServerConnector); !ok {
		t.Error("expected ServerConnector")
	}

	// Test creating client
	clientCfg := &connector.Config{
		Name:   "test-client",
		Type:   "graphql",
		Driver: "client",
		Properties: map[string]interface{}{
			"endpoint": "http://localhost:4000/graphql",
		},
	}

	conn, err = factory.Create(ctx, clientCfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, ok := conn.(*ClientConnector); !ok {
		t.Error("expected ClientConnector")
	}
}

// TestClientRetry tests retry functionality.
func TestClientRetry(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		response := GraphQLResponse{Data: map[string]interface{}{"test": true}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &ClientConfig{
		Endpoint:   server.URL,
		Timeout:    5 * time.Second,
		RetryCount: 3,
		RetryDelay: 10 * time.Millisecond,
	}

	client := NewClient("test-retry", config)
	ctx := context.Background()
	client.Connect(ctx)

	_, err := client.Read(ctx, connector.Query{Target: "{ test }"})
	if err != nil {
		t.Fatalf("Read should succeed after retries: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// TestGraphQLErrorHandling tests GraphQL error response handling.
func TestGraphQLErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GraphQLResponse{
			Errors: []GraphQLError{
				{Message: "Field 'unknown' not found"},
				{Message: "Syntax error"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &ClientConfig{
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
	}

	client := NewClient("test-errors", config)
	ctx := context.Background()
	client.Connect(ctx)

	_, err := client.Read(ctx, connector.Query{Target: "{ unknown }"})
	if err == nil {
		t.Fatal("expected error for GraphQL errors")
	}

	if !strings.Contains(err.Error(), "Field 'unknown' not found") {
		t.Errorf("expected error message to contain 'Field 'unknown' not found', got '%s'", err.Error())
	}
}

// TestMapArgsToInput tests argument mapping.
func TestMapArgsToInput(t *testing.T) {
	// Simulate ResolveParams with arguments
	// Since graphql.ResolveParams requires complex setup,
	// we test the resolver helper functions indirectly
	// through integration tests

	// Test basic resolver creation
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return input, nil
	}

	resolver := CreateResolver(handler)
	if resolver == nil {
		t.Fatal("resolver should not be nil")
	}
}

// TestBuildErrorResponse tests error response building.
func TestBuildErrorResponse(t *testing.T) {
	err := fmt.Errorf("test error")
	response := BuildErrorResponse(err)

	if len(response.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(response.Errors))
	}

	if response.Errors[0].Message != "test error" {
		t.Errorf("expected message 'test error', got '%s'", response.Errors[0].Message)
	}
}

// TestFederationSupport tests Federation v2 support.
func TestFederationSupport(t *testing.T) {
	t.Run("NewFederationSupport creates with default version 2", func(t *testing.T) {
		fed := NewFederationSupport(0)
		if fed.version != 2 {
			t.Errorf("expected version 2, got %d", fed.version)
		}
	})

	t.Run("SetSDL and GetSDL work correctly", func(t *testing.T) {
		fed := NewFederationSupport(2)
		fed.SetSDL("type Query { hello: String }")

		sdl := fed.GetSDL()
		if sdl != "type Query { hello: String }" {
			t.Errorf("SDL mismatch: %s", sdl)
		}
	})

	t.Run("RegisterEntity and GetEntityNames work correctly", func(t *testing.T) {
		fed := NewFederationSupport(2)

		keys := []EntityKey{{Fields: "id", Resolvable: true}}
		resolver := func(ctx context.Context, rep map[string]interface{}) (interface{}, error) {
			return rep, nil
		}

		fed.RegisterEntity("User", keys, resolver, nil)

		names := fed.GetEntityNames()
		if len(names) != 1 || names[0] != "User" {
			t.Errorf("expected [User], got %v", names)
		}

		if !fed.HasEntities() {
			t.Error("HasEntities should return true")
		}
	})

	t.Run("CreateServiceField returns _service field", func(t *testing.T) {
		fed := NewFederationSupport(2)
		fed.SetSDL("type Query { test: String }")

		field := fed.CreateServiceField()
		if field == nil {
			t.Fatal("_service field should not be nil")
		}
	})

	t.Run("CreateEntitiesField returns _entities field", func(t *testing.T) {
		fed := NewFederationSupport(2)

		field := fed.CreateEntitiesField()
		if field == nil {
			t.Fatal("_entities field should not be nil")
		}
	})
}

// TestSchemaBuilderWithFederation tests schema builder with Federation enabled.
func TestSchemaBuilderWithFederation(t *testing.T) {
	t.Run("EnableFederation enables federation support", func(t *testing.T) {
		builder := NewSchemaBuilder()
		builder.EnableFederation(2)

		if !builder.IsFederationEnabled() {
			t.Error("Federation should be enabled")
		}
	})

	t.Run("Build includes _service field when federation enabled", func(t *testing.T) {
		builder := NewSchemaBuilder()
		builder.EnableFederation(2)

		// Register a query handler
		handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return []map[string]interface{}{{"id": 1}}, nil
		}
		builder.RegisterHandler("Query.users", handler)

		schema, err := builder.Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		if schema == nil {
			t.Fatal("schema should not be nil")
		}

		// Verify _service field exists by introspecting the schema
		queryType := schema.QueryType()
		if queryType == nil {
			t.Fatal("Query type should exist")
		}

		serviceField := queryType.Fields()["_service"]
		if serviceField == nil {
			t.Error("_service field should exist in Query type")
		}

		entitiesField := queryType.Fields()["_entities"]
		if entitiesField == nil {
			t.Error("_entities field should exist in Query type")
		}
	})

	t.Run("RegisterEntity adds entity to federation", func(t *testing.T) {
		builder := NewSchemaBuilder()
		builder.EnableFederation(2)

		keys := []EntityKey{{Fields: "id", Resolvable: true}}
		resolver := func(ctx context.Context, rep map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"id": rep["id"], "name": "Test"}, nil
		}

		builder.RegisterEntity("User", keys, resolver)

		fed := builder.GetFederation()
		if !fed.HasEntities() {
			t.Error("Federation should have entities")
		}

		names := fed.GetEntityNames()
		found := false
		for _, n := range names {
			if n == "User" {
				found = true
				break
			}
		}
		if !found {
			t.Error("User entity should be registered")
		}
	})

	t.Run("generateSDL includes Federation directives", func(t *testing.T) {
		builder := NewSchemaBuilder()
		builder.EnableFederation(2)

		handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return nil, nil
		}
		builder.RegisterHandler("Query.test", handler)

		// Build to trigger SDL generation
		_, err := builder.Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		fed := builder.GetFederation()
		sdl := fed.GetSDL()

		if !strings.Contains(sdl, "@key") {
			t.Error("SDL should contain @key directive")
		}

		if !strings.Contains(sdl, "@shareable") {
			t.Error("SDL should contain @shareable directive")
		}

		if !strings.Contains(sdl, "FieldSet") {
			t.Error("SDL should contain FieldSet scalar")
		}
	})
}

// TestServerWithFederation tests server connector with Federation enabled.
func TestServerWithFederation(t *testing.T) {
	config := &ServerConfig{
		Port:     0,
		Host:     "localhost",
		Endpoint: "/graphql",
		Federation: &FederationServerConfig{
			Enabled: true,
			Version: 2,
		},
	}

	server := NewServer("test-federation", config, nil)

	ctx := context.Background()
	if err := server.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !server.IsFederationEnabled() {
		t.Error("Federation should be enabled")
	}

	// Register a handler
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return []map[string]interface{}{{"id": 1, "name": "Test"}}, nil
	}
	server.RegisterRoute("Query.users", handler)

	// Register an entity
	entityResolver := func(ctx context.Context, rep map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"__typename": "User",
			"id":         rep["id"],
			"name":       "Resolved User",
		}, nil
	}
	server.RegisterEntity("User", []EntityKey{{Fields: "id", Resolvable: true}}, entityResolver)

	// Close
	if err := server.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestFactoryWithFederation tests factory creates server with Federation.
func TestFactoryWithFederation(t *testing.T) {
	factory := NewFactory(nil)

	ctx := context.Background()
	cfg := &connector.Config{
		Name:   "test-federation-server",
		Type:   "graphql",
		Driver: "server",
		Properties: map[string]interface{}{
			"port":       4000,
			"playground": true,
			"federation": map[string]interface{}{
				"enabled": true,
				"version": 2,
			},
		},
	}

	conn, err := factory.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	server, ok := conn.(*ServerConnector)
	if !ok {
		t.Fatal("expected ServerConnector")
	}

	if err := server.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !server.IsFederationEnabled() {
		t.Error("Federation should be enabled")
	}
}

// TestGetFederationDirectives tests directive generation.
func TestGetFederationDirectives(t *testing.T) {
	t.Run("Federation v1 directives", func(t *testing.T) {
		directives := GetFederationDirectives(1)

		if !strings.Contains(directives, "@key") {
			t.Error("v1 should contain @key directive")
		}

		if !strings.Contains(directives, "@external") {
			t.Error("v1 should contain @external directive")
		}

		// v1 should NOT have @shareable
		if strings.Contains(directives, "@shareable") {
			t.Error("v1 should not contain @shareable directive")
		}
	})

	t.Run("Federation v2 directives", func(t *testing.T) {
		directives := GetFederationDirectives(2)

		if !strings.Contains(directives, "@key") {
			t.Error("v2 should contain @key directive")
		}

		if !strings.Contains(directives, "@shareable") {
			t.Error("v2 should contain @shareable directive")
		}

		if !strings.Contains(directives, "@override") {
			t.Error("v2 should contain @override directive")
		}

		if !strings.Contains(directives, "@inaccessible") {
			t.Error("v2 should contain @inaccessible directive")
		}

		if !strings.Contains(directives, "extend schema") {
			t.Error("v2 should contain extend schema with @link")
		}
	})
}

// TestParseFederationDirectives tests parsing @key directives from SDL.
func TestParseFederationDirectives(t *testing.T) {
	sdl := `
type User @key(fields: "id") {
  id: ID!
  name: String!
}

type Product @key(fields: "sku") @key(fields: "id", resolvable: false) {
  id: ID!
  sku: String!
  name: String!
}
`

	entities := ParseFederationDirectives(sdl)

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	userKeys, ok := entities["User"]
	if !ok {
		t.Fatal("User entity should be parsed")
	}
	if len(userKeys) != 1 || userKeys[0].Fields != "id" {
		t.Errorf("User should have key 'id', got %v", userKeys)
	}

	productKeys, ok := entities["Product"]
	if !ok {
		t.Fatal("Product entity should be parsed")
	}
	if len(productKeys) != 2 {
		t.Errorf("Product should have 2 keys, got %d", len(productKeys))
	}
}

// TestSubscriptionField tests subscription field registration and PubSub delivery.
func TestSubscriptionField(t *testing.T) {
	builder := NewSchemaBuilder()

	// Register a subscription handler
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return input, nil
	}

	if err := builder.RegisterHandler("Subscription.orderUpdated", handler); err != nil {
		t.Fatalf("failed to register subscription handler: %v", err)
	}

	// Verify field was registered
	if !builder.HasField("Subscription.orderUpdated") {
		t.Fatal("subscription field should be registered")
	}

	// Build schema — should include Subscription type
	schema, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	if schema.SubscriptionType() == nil {
		t.Fatal("schema should have Subscription type")
	}

	fields := schema.SubscriptionType().Fields()
	if _, ok := fields["orderUpdated"]; !ok {
		t.Fatal("schema should have orderUpdated subscription field")
	}
}

// TestSubscriptionFieldWithReturnType tests subscription with typed return.
func TestSubscriptionFieldWithReturnType(t *testing.T) {
	builder := NewSchemaBuilder()

	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return input, nil
	}

	if err := builder.RegisterHandlerWithArgs("Subscription.productUpdated", handler, "JSON", nil); err != nil {
		t.Fatalf("failed to register subscription handler: %v", err)
	}

	if !builder.HasField("Subscription.productUpdated") {
		t.Fatal("subscription field should be registered")
	}

	schema, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	if schema.SubscriptionType() == nil {
		t.Fatal("schema should have Subscription type")
	}
}

// TestPubSubBasic tests basic PubSub publish/subscribe.
func TestPubSubBasic(t *testing.T) {
	ps := NewPubSub()

	ch := ps.Subscribe("test-topic")

	// Publish a message
	ps.Publish("test-topic", map[string]interface{}{"id": 1})

	// Should receive it
	select {
	case data := <-ch:
		m, ok := data.(map[string]interface{})
		if !ok {
			t.Fatal("expected map")
		}
		if m["id"] != 1 {
			t.Errorf("expected id=1, got %v", m["id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}

	// Unsubscribe
	ps.Unsubscribe("test-topic", ch)

	// Publish after unsubscribe — should not panic
	ps.Publish("test-topic", map[string]interface{}{"id": 2})
}

// TestPubSubWithFilter tests filtered subscription delivery.
func TestPubSubWithFilter(t *testing.T) {
	ps := NewPubSub()

	// Subscribe with filter: only accept messages where status == "shipped"
	ch := ps.SubscribeWithFilter("orders", func(data interface{}) bool {
		if m, ok := data.(map[string]interface{}); ok {
			return m["status"] == "shipped"
		}
		return false
	})

	// Publish a message that doesn't match filter
	ps.Publish("orders", map[string]interface{}{"id": 1, "status": "pending"})

	// Should NOT receive it
	select {
	case <-ch:
		t.Fatal("should not receive filtered-out message")
	case <-time.After(50 * time.Millisecond):
		// Expected — no message
	}

	// Publish a message that matches filter
	ps.Publish("orders", map[string]interface{}{"id": 2, "status": "shipped"})

	// Should receive it
	select {
	case data := <-ch:
		m, ok := data.(map[string]interface{})
		if !ok {
			t.Fatal("expected map")
		}
		if m["id"] != 2 {
			t.Errorf("expected id=2, got %v", m["id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for filtered message")
	}

	ps.Unsubscribe("orders", ch)
}

// TestPubSubMultipleSubscribers tests multiple subscribers on one topic.
func TestPubSubMultipleSubscribers(t *testing.T) {
	ps := NewPubSub()

	ch1 := ps.Subscribe("events")
	ch2 := ps.Subscribe("events")

	ps.Publish("events", "hello")

	for _, ch := range []chan interface{}{ch1, ch2} {
		select {
		case data := <-ch:
			if data != "hello" {
				t.Errorf("expected 'hello', got %v", data)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for message")
		}
	}

	ps.Unsubscribe("events", ch1)
	ps.Unsubscribe("events", ch2)
}

// TestSubscriptionSDLGeneration tests that SDL includes Subscription type.
func TestSubscriptionSDLGeneration(t *testing.T) {
	builder := NewSchemaBuilder()
	builder.EnableFederation(2)

	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return input, nil
	}

	// Register a query and a subscription
	builder.RegisterHandler("Query.users", handler)
	builder.RegisterHandler("Subscription.userCreated", handler)

	// Build to trigger SDL generation
	_, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	sdl := builder.GetFederation().GetSDL()

	if !strings.Contains(sdl, "type Subscription") {
		t.Error("SDL should contain Subscription type")
	}
	if !strings.Contains(sdl, "userCreated") {
		t.Error("SDL should contain userCreated field")
	}
}

// TestServerConnectorPublish tests the Publish method on ServerConnector.
func TestServerConnectorPublish(t *testing.T) {
	config := &ServerConfig{
		Port:     0,
		Endpoint: "/graphql",
		Subscriptions: &SubscriptionsConfig{
			Enabled: true,
			Path:    "/subscriptions",
		},
	}

	server := NewServer("test", config, nil)

	// Register a subscription
	server.RegisterSubscription("orderUpdated", "")

	// Get the pubsub from the schema builder
	ps := server.schemaBuilder.GetPubSub()

	// Subscribe directly to verify publish works
	ch := ps.Subscribe("orderUpdated")

	// Publish through the server
	server.Publish("orderUpdated", map[string]interface{}{"id": 1})

	select {
	case data := <-ch:
		m, ok := data.(map[string]interface{})
		if !ok {
			t.Fatal("expected map")
		}
		if m["id"] != 1 {
			t.Errorf("expected id=1, got %v", m["id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for published message")
	}

	ps.Unsubscribe("orderUpdated", ch)
}

// TestSubscriptionFilter tests SetSubscriptionFilter on SchemaBuilder.
func TestSubscriptionFilter(t *testing.T) {
	builder := NewSchemaBuilder()

	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return input, nil
	}

	builder.RegisterHandler("Subscription.orderUpdated", handler)
	builder.SetSubscriptionFilter("orderUpdated", "input.user_id == context.auth.user_id")

	// Verify filter was stored
	if builder.subscriptionFilters["orderUpdated"] != "input.user_id == context.auth.user_id" {
		t.Error("subscription filter should be stored")
	}
}

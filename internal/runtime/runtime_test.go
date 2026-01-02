package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestIntegration_TransformOnCreate tests that transforms are applied during POST requests.
func TestIntegration_TransformOnCreate(t *testing.T) {
	// Create temp directory for test config
	tmpDir, err := os.MkdirTemp("", "mycel-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Setup SQLite database
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	// Create test configuration with transforms
	port := 3901 // Use a non-standard port for tests
	createTestConfig(t, tmpDir, dbPath, port)

	// Start the runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	// Wait for server to be ready
	waitForServer(t, port)

	// Test: Create user with transform applied
	t.Run("POST with transform lowercases email", func(t *testing.T) {
		payload := map[string]interface{}{
			"email": "UPPERCASE@EXAMPLE.COM",
			"name":  "  Test User  ",
		}

		resp, body := doRequest(t, "POST", fmt.Sprintf("http://localhost:%d/users", port), payload)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify response contains ID
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if result["id"] == nil {
			t.Error("expected id in response")
		}

		// Verify the email was lowercased and name was trimmed in the database
		var email, name string
		err := db.QueryRow("SELECT email, name FROM users WHERE id = ?", int(result["id"].(float64))).Scan(&email, &name)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		if email != "uppercase@example.com" {
			t.Errorf("expected email to be lowercased, got %s", email)
		}

		if name != "Test User" {
			t.Errorf("expected name to be trimmed, got '%s'", name)
		}
	})

	// Test: Create user with nested transform
	t.Run("POST with uuid transform generates ID", func(t *testing.T) {
		payload := map[string]interface{}{
			"email": "uuid-test@example.com",
			"name":  "UUID Test",
		}

		resp, body := doRequest(t, "POST", fmt.Sprintf("http://localhost:%d/users-uuid", port), payload)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify the external_id was generated as UUID
		var externalID string
		err := db.QueryRow("SELECT external_id FROM users ORDER BY id DESC LIMIT 1").Scan(&externalID)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
		if len(externalID) != 36 {
			t.Errorf("expected external_id to be UUID (36 chars), got '%s' (%d chars)", externalID, len(externalID))
		}
	})
}

// TestIntegration_ValidationOnCreate tests that validation is applied during POST requests.
func TestIntegration_ValidationOnCreate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-validation-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	port := 3902
	createTestConfigWithValidation(t, tmpDir, dbPath, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForServer(t, port)

	t.Run("validation rejects invalid email format", func(t *testing.T) {
		payload := map[string]interface{}{
			"email": "invalid-email",
			"name":  "Test User",
		}

		resp, body := doRequest(t, "POST", fmt.Sprintf("http://localhost:%d/validated-users", port), payload)
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("expected validation error, got 200: %s", body)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("validation rejects name too short", func(t *testing.T) {
		payload := map[string]interface{}{
			"email": "valid@example.com",
			"name":  "", // empty name
		}

		resp, body := doRequest(t, "POST", fmt.Sprintf("http://localhost:%d/validated-users", port), payload)
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("expected validation error, got 200: %s", body)
		}
	})

	t.Run("validation accepts valid data", func(t *testing.T) {
		payload := map[string]interface{}{
			"email": "valid@example.com",
			"name":  "Valid Name",
		}

		resp, body := doRequest(t, "POST", fmt.Sprintf("http://localhost:%d/validated-users", port), payload)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
	})
}

// TestIntegration_GetWithFilters tests that GET requests with path params work correctly.
func TestIntegration_GetWithFilters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-get-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	// Insert test data
	_, err = db.Exec("INSERT INTO users (email, name) VALUES ('test1@example.com', 'Test One'), ('test2@example.com', 'Test Two')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	port := 3903
	createTestConfig(t, tmpDir, dbPath, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForServer(t, port)

	t.Run("GET /users returns all users", func(t *testing.T) {
		resp, body := doRequest(t, "GET", fmt.Sprintf("http://localhost:%d/users", port), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var users []map[string]interface{}
		if err := json.Unmarshal([]byte(body), &users); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}
	})

	t.Run("GET /users/:id returns single user", func(t *testing.T) {
		resp, body := doRequest(t, "GET", fmt.Sprintf("http://localhost:%d/users/1", port), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var users []map[string]interface{}
		if err := json.Unmarshal([]byte(body), &users); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(users) != 1 {
			t.Errorf("expected 1 user, got %d", len(users))
		}
	})
}

// Helper functions

func setupTestDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Create users table with external_id for UUID tests
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			name TEXT NOT NULL,
			external_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func createTestConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	// Create directories
	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "types"), 0755)

	// Main config
	writeFile(t, filepath.Join(tmpDir, "config.hcl"), fmt.Sprintf(`
service {
  name    = "test-service"
  version = "1.0.0"
}
`))

	// REST connector
	writeFile(t, filepath.Join(tmpDir, "connectors", "api.hcl"), fmt.Sprintf(`
connector "api" {
  type = "rest"
  port = %d
}
`, port))

	// SQLite connector
	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Flows with transforms
	writeFile(t, filepath.Join(tmpDir, "flows", "users.hcl"), `
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    email = "lower(input.email)"
    name  = "trim(input.name)"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "create_user_uuid" {
  from {
    connector = "api"
    operation = "POST /users-uuid"
  }

  transform {
    email       = "lower(input.email)"
    name        = "input.name"
    external_id = "uuid()"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}
`)
}

func createTestConfigWithValidation(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "types"), 0755)

	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "validation-test"
  version = "1.0.0"
}
`)

	writeFile(t, filepath.Join(tmpDir, "connectors", "api.hcl"), fmt.Sprintf(`
connector "api" {
  type = "rest"
  port = %d
}
`, port))

	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Type with constraints - using function call syntax for constraints
	// string({ format = "email" }) is how HCL parses string { format = "email" }
	writeFile(t, filepath.Join(tmpDir, "types", "user.hcl"), `
type "validated_user" {
  email = string({ format = "email" })
  name  = string({ min_length = 1 })
}
`)

	// Flow with validation
	writeFile(t, filepath.Join(tmpDir, "flows", "users.hcl"), `
flow "create_validated_user" {
  from {
    connector = "api"
    operation = "POST /validated-users"
  }

  validate {
    input = "validated_user"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}
`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func startTestRuntime(ctx context.Context, configDir string) (*Runtime, error) {
	rt, err := New(Options{
		ConfigDir:       configDir,
		Environment:     "test",
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	// Start in a goroutine
	go func() {
		if err := rt.Start(ctx); err != nil && err != context.Canceled {
			fmt.Printf("runtime error: %v\n", err)
		}
	}()

	return rt, nil
}

func waitForServer(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("server did not start within timeout on port %d", port)
}

func doRequest(t *testing.T, method, url string, payload interface{}) (*http.Response, string) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}
		body = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	return resp, string(respBody)
}

// =============================================================================
// GraphQL Integration Tests - Dynamic Mode (Basic)
// =============================================================================

// TestIntegration_GraphQL_Dynamic tests GraphQL server with dynamic schema generation.
// This is the simplest mode where types are created automatically as JSON.
func TestIntegration_GraphQL_Dynamic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-graphql-dynamic-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	// Insert test data
	_, err = db.Exec("INSERT INTO users (email, name) VALUES ('john@example.com', 'John Doe'), ('jane@example.com', 'Jane Doe')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	gqlPort := 4901
	createGraphQLDynamicConfig(t, tmpDir, dbPath, gqlPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForGraphQLServer(t, gqlPort)

	t.Run("Query.users returns all users as JSON array", func(t *testing.T) {
		query := `{ "query": "{ users }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		result := parseGraphQLResponse(t, body)
		users := result["users"].([]interface{})

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}
	})

	t.Run("Mutation.createUser creates user with transforms", func(t *testing.T) {
		query := `{ "query": "mutation { createUser(input: { email: \"DYNAMIC@TEST.COM\", name: \"  Dynamic User  \" }) }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify in database
		var email, name string
		err := db.QueryRow("SELECT email, name FROM users ORDER BY id DESC LIMIT 1").Scan(&email, &name)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		if email != "dynamic@test.com" {
			t.Errorf("expected email 'dynamic@test.com', got '%s'", email)
		}
		if name != "Dynamic User" {
			t.Errorf("expected name 'Dynamic User', got '%s'", name)
		}
	})

	t.Run("Playground is accessible", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/playground", gqlPort))
		if err != nil {
			t.Fatalf("failed to access playground: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for playground, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !bytes.Contains(body, []byte("GraphQL Playground")) {
			t.Error("expected playground HTML response")
		}
	})
}

func createGraphQLDynamicConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)

	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "graphql-dynamic-test"
  version = "1.0.0"
}
`)

	writeFile(t, filepath.Join(tmpDir, "connectors", "graphql.hcl"), fmt.Sprintf(`
connector "gql" {
  type   = "graphql"
  driver = "server"
  port       = %d
  endpoint   = "/graphql"
  playground = true
}
`, port))

	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	writeFile(t, filepath.Join(tmpDir, "flows", "graphql.hcl"), `
flow "get_users" {
  from {
    connector = "gql"
    operation = "Query.users"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "gql"
    operation = "Mutation.createUser"
  }
  transform {
    email = "lower(input.email)"
    name  = "trim(input.name)"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}
`)
}

// =============================================================================
// GraphQL Helper Functions
// =============================================================================

func waitForGraphQLServer(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("GraphQL server did not start within timeout on port %d", port)
}

func doGraphQLRequest(t *testing.T, port int, query string) (*http.Response, string) {
	t.Helper()

	url := fmt.Sprintf("http://localhost:%d/graphql", port)
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(query)))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	return resp, string(body)
}

// parseGraphQLResponse parses a GraphQL response and returns the data map.
// It fails the test if there are errors in the response.
func parseGraphQLResponse(t *testing.T, body string) map[string]interface{} {
	t.Helper()

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to parse GraphQL response: %v", err)
	}

	if errs, ok := result["errors"]; ok {
		t.Fatalf("GraphQL errors: %v", errs)
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data in GraphQL response, got: %s", body)
	}

	return data
}

// =============================================================================
// GraphQL Integration Tests - Schema-First Mode (Full Suite)
// =============================================================================

// TestIntegration_GraphQL_SchemaFirst_CRUD tests complete CRUD operations with Schema-first mode.
// This mirrors the REST integration tests with full type support from SDL.
func TestIntegration_GraphQL_SchemaFirst_CRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-graphql-sdl-crud-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	// Insert test data
	_, err = db.Exec("INSERT INTO users (email, name) VALUES ('john@example.com', 'John Doe'), ('jane@example.com', 'Jane Doe')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	gqlPort := 4910
	createGraphQLSchemaFirstFullConfig(t, tmpDir, dbPath, gqlPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForGraphQLServer(t, gqlPort)

	// =========================================================================
	// READ Operations
	// =========================================================================

	t.Run("Query.users returns all users with typed fields", func(t *testing.T) {
		query := `{ "query": "{ users { id email name createdAt } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		users := data["users"].([]interface{})

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}

		// Verify typed fields
		user := users[0].(map[string]interface{})
		if _, ok := user["id"]; !ok {
			t.Error("expected 'id' field")
		}
		if _, ok := user["email"]; !ok {
			t.Error("expected 'email' field")
		}
		if _, ok := user["name"]; !ok {
			t.Error("expected 'name' field")
		}
	})

	t.Run("Query.user returns single user by ID (smart unwrap)", func(t *testing.T) {
		query := `{ "query": "{ user(id: 1) { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		user := data["user"]

		// Should be unwrapped to single object (not array)
		userObj, ok := user.(map[string]interface{})
		if !ok {
			t.Fatalf("expected single user object (smart unwrap), got %T", user)
		}

		if userObj["email"] != "john@example.com" {
			t.Errorf("expected email 'john@example.com', got '%v'", userObj["email"])
		}
	})

	t.Run("Query.user returns null for non-existent ID", func(t *testing.T) {
		query := `{ "query": "{ user(id: 9999) { id email } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		user := data["user"]

		if user != nil {
			t.Errorf("expected null for non-existent user, got %v", user)
		}
	})

	// =========================================================================
	// CREATE Operations
	// =========================================================================

	t.Run("Mutation.createUser with transforms returns created user", func(t *testing.T) {
		query := `{ "query": "mutation { createUser(input: { email: \"CREATE@TEST.COM\", name: \"  Created User  \" }) { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUser"].(map[string]interface{})

		// Verify transform applied: email lowercased
		if created["email"] != "create@test.com" {
			t.Errorf("expected email 'create@test.com', got '%v'", created["email"])
		}

		// Verify transform applied: name trimmed
		if created["name"] != "Created User" {
			t.Errorf("expected name 'Created User', got '%v'", created["name"])
		}

		// Verify ID returned
		if created["id"] == nil {
			t.Error("expected 'id' in response")
		}

		// Verify in database
		var dbEmail, dbName string
		err := db.QueryRow("SELECT email, name FROM users ORDER BY id DESC LIMIT 1").Scan(&dbEmail, &dbName)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		if dbEmail != "create@test.com" {
			t.Errorf("expected DB email 'create@test.com', got '%s'", dbEmail)
		}
		if dbName != "Created User" {
			t.Errorf("expected DB name 'Created User', got '%s'", dbName)
		}
	})

	t.Run("Mutation.createUserWithUUID generates UUID and returns externalId", func(t *testing.T) {
		query := `{ "query": "mutation { createUserWithUUID(input: { email: \"uuid@test.com\", name: \"UUID User\" }) { id email externalId } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUserWithUUID"].(map[string]interface{})

		// Verify ID returned
		if created["id"] == nil {
			t.Error("expected 'id' in response")
		}

		// Verify externalId is returned in GraphQL response (snake_case -> camelCase mapping)
		externalId, ok := created["externalId"].(string)
		if !ok || externalId == "" {
			t.Errorf("expected 'externalId' as string in response, got '%v'", created["externalId"])
		}

		// Verify UUID format (36 chars with dashes)
		if len(externalId) != 36 {
			t.Errorf("expected UUID (36 chars), got '%s' (%d chars)", externalId, len(externalId))
		}

		// Verify UUID in database matches
		var dbExternalID string
		err := db.QueryRow("SELECT external_id FROM users ORDER BY id DESC LIMIT 1").Scan(&dbExternalID)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}
		if dbExternalID != externalId {
			t.Errorf("GraphQL externalId '%s' doesn't match DB external_id '%s'", externalId, dbExternalID)
		}
	})

	// =========================================================================
	// UPDATE Operations
	// =========================================================================

	t.Run("Mutation.updateUser updates user and returns updated data", func(t *testing.T) {
		// First create a user to update
		createQuery := `{ "query": "mutation { createUser(input: { email: \"update@test.com\", name: \"Original Name\" }) { id } }" }`
		_, createBody := doGraphQLRequest(t, gqlPort, createQuery)
		createData := parseGraphQLResponse(t, createBody)
		userID := createData["createUser"].(map[string]interface{})["id"]

		// Update the user
		updateQuery := fmt.Sprintf(`{ "query": "mutation { updateUser(id: \"%v\", input: { name: \"Updated Name\" }) { id email name } }" }`, userID)
		resp, body := doGraphQLRequest(t, gqlPort, updateQuery)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify in database
		var dbName string
		err := db.QueryRow("SELECT name FROM users WHERE id = ?", userID).Scan(&dbName)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}
		if dbName != "Updated Name" {
			t.Errorf("expected name 'Updated Name' in DB, got '%s'", dbName)
		}
	})

	// =========================================================================
	// DELETE Operations
	// =========================================================================

	t.Run("Mutation.deleteUser deletes user and returns result", func(t *testing.T) {
		// First create a user to delete
		createQuery := `{ "query": "mutation { createUser(input: { email: \"delete@test.com\", name: \"To Delete\" }) { id } }" }`
		_, createBody := doGraphQLRequest(t, gqlPort, createQuery)
		createData := parseGraphQLResponse(t, createBody)
		userID := createData["createUser"].(map[string]interface{})["id"]

		// Count before delete
		var countBefore int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&countBefore)

		// Delete the user - GraphQL returns ID as string
		deleteQuery := fmt.Sprintf(`{ "query": "mutation { deleteUser(id: \"%v\") }" }`, userID)
		resp, body := doGraphQLRequest(t, gqlPort, deleteQuery)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify user was deleted
		var countAfter int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&countAfter)

		if countAfter >= countBefore {
			t.Errorf("expected count to decrease, before: %d, after: %d", countBefore, countAfter)
		}

		// Verify user doesn't exist - userID is a string
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}
		if exists {
			t.Error("expected user to be deleted")
		}
	})

	// =========================================================================
	// Schema Introspection
	// =========================================================================

	t.Run("Introspection returns User type with all fields", func(t *testing.T) {
		query := `{ "query": "{ __type(name: \"User\") { name fields { name type { name kind ofType { name } } } } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		typeInfo := data["__type"].(map[string]interface{})

		if typeInfo["name"] != "User" {
			t.Errorf("expected type name 'User', got '%v'", typeInfo["name"])
		}

		fields := typeInfo["fields"].([]interface{})
		fieldNames := make(map[string]bool)
		for _, f := range fields {
			field := f.(map[string]interface{})
			fieldNames[field["name"].(string)] = true
		}

		expectedFields := []string{"id", "email", "name"}
		for _, expected := range expectedFields {
			if !fieldNames[expected] {
				t.Errorf("expected field '%s' in User type", expected)
			}
		}
	})

	t.Run("Playground is accessible", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/playground", gqlPort))
		if err != nil {
			t.Fatalf("failed to access playground: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for playground, got %d", resp.StatusCode)
		}
	})

	// =========================================================================
	// GraphQL Variables Tests
	// =========================================================================

	t.Run("Query with variables works correctly", func(t *testing.T) {
		// Note: Variables work at GraphQL level, not as SQL parameters
		// The variable value is passed to the resolver which uses it as filter
		query := `{
			"query": "query GetUser($id: ID!) { user(id: $id) { id email name } }",
			"variables": { "id": "1" }
		}`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		user := data["user"].(map[string]interface{})
		if user["email"] != "john@example.com" {
			t.Errorf("expected email 'john@example.com', got '%v'", user["email"])
		}
	})

	t.Run("Mutation with variables works correctly", func(t *testing.T) {
		query := `{
			"query": "mutation CreateUser($input: CreateUserInput!) { createUser(input: $input) { id email name } }",
			"variables": { "input": { "email": "VARIABLE@TEST.COM", "name": "  Variable User  " } }
		}`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUser"].(map[string]interface{})

		// Verify transforms applied via variables
		if created["email"] != "variable@test.com" {
			t.Errorf("expected email 'variable@test.com', got '%v'", created["email"])
		}
		if created["name"] != "Variable User" {
			t.Errorf("expected name 'Variable User', got '%v'", created["name"])
		}
	})

	// =========================================================================
	// Error Handling Tests
	// =========================================================================

	t.Run("Invalid query returns GraphQL error", func(t *testing.T) {
		query := `{ "query": "{ invalidField }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for GraphQL error, got %d", resp.StatusCode)
		}

		// GraphQL errors are returned with 200 status, check errors field
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		errors, hasErrors := result["errors"]
		if !hasErrors || errors == nil {
			t.Error("expected GraphQL errors for invalid query")
		}
	})

	t.Run("Missing required input field returns error", func(t *testing.T) {
		// CreateUserInput requires email and name
		query := `{ "query": "mutation { createUser(input: { email: \"only@email.com\" }) { id } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		// Should have error for missing required field
		errors, hasErrors := result["errors"]
		if !hasErrors || errors == nil {
			t.Error("expected GraphQL errors for missing required field")
		}
	})

	t.Run("Empty query returns error", func(t *testing.T) {
		query := `{ "query": "" }`
		resp, _ := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for empty query, got %d", resp.StatusCode)
		}
	})
}

func createGraphQLSchemaFirstFullConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "schema"), 0755)

	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "graphql-sdl-crud-test"
  version = "1.0.0"
}
`)

	// Complete SDL Schema with all CRUD operations
	writeFile(t, filepath.Join(tmpDir, "schema", "schema.graphql"), `
type User {
  id: ID!
  email: String!
  name: String!
  externalId: String
  createdAt: String
}

input CreateUserInput {
  email: String!
  name: String!
}

input UpdateUserInput {
  email: String
  name: String
}

scalar JSON

type Query {
  users: [User!]!
  user(id: ID!): User
}

type Mutation {
  createUser(input: CreateUserInput!): User
  createUserWithUUID(input: CreateUserInput!): User
  updateUser(id: ID!, input: UpdateUserInput!): User
  deleteUser(id: ID!): JSON
}
`)

	schemaPath := filepath.Join(tmpDir, "schema", "schema.graphql")
	writeFile(t, filepath.Join(tmpDir, "connectors", "graphql.hcl"), fmt.Sprintf(`
connector "gql" {
  type   = "graphql"
  driver = "server"
  port       = %d
  endpoint   = "/graphql"
  playground = true
  schema {
    path = "%s"
  }
}
`, port, schemaPath))

	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Complete flows for CRUD
	writeFile(t, filepath.Join(tmpDir, "flows", "graphql.hcl"), `
# READ: Get all users
flow "get_users" {
  from {
    connector = "gql"
    operation = "Query.users"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# READ: Get single user by ID
flow "get_user" {
  from {
    connector = "gql"
    operation = "Query.user"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# CREATE: Create user with transforms
flow "create_user" {
  from {
    connector = "gql"
    operation = "Mutation.createUser"
  }
  transform {
    email = "lower(input.email)"
    name  = "trim(input.name)"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# CREATE: Create user with UUID
flow "create_user_uuid" {
  from {
    connector = "gql"
    operation = "Mutation.createUserWithUUID"
  }
  transform {
    email       = "input.email"
    name        = "input.name"
    external_id = "uuid()"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# UPDATE: Update user
flow "update_user" {
  from {
    connector = "gql"
    operation = "Mutation.updateUser"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# DELETE: Delete user
flow "delete_user" {
  from {
    connector = "gql"
    operation = "Mutation.deleteUser"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}
`)
}

// =============================================================================
// GraphQL Integration Tests - HCL-First Mode (Full Suite)
// =============================================================================

// TestIntegration_GraphQL_HCLFirst_CRUD tests complete CRUD operations with HCL-first mode.
// Types are defined in HCL and flows use the 'returns' attribute.
func TestIntegration_GraphQL_HCLFirst_CRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-graphql-hcl-crud-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	// Insert test data
	_, err = db.Exec("INSERT INTO users (email, name) VALUES ('alice@example.com', 'Alice'), ('bob@example.com', 'Bob')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	gqlPort := 4920
	createGraphQLHCLFirstFullConfig(t, tmpDir, dbPath, gqlPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForGraphQLServer(t, gqlPort)

	// =========================================================================
	// READ Operations
	// =========================================================================

	t.Run("Query.users returns User[] type from HCL", func(t *testing.T) {
		query := `{ "query": "{ users { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		users := data["users"].([]interface{})

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}

		// Verify typed fields from HCL type
		user := users[0].(map[string]interface{})
		if _, ok := user["id"]; !ok {
			t.Error("expected 'id' field from HCL type")
		}
		if _, ok := user["email"]; !ok {
			t.Error("expected 'email' field from HCL type")
		}
		if _, ok := user["name"]; !ok {
			t.Error("expected 'name' field from HCL type")
		}
	})

	t.Run("Query.user returns single User with correct data", func(t *testing.T) {
		// First get all users to find a valid ID and its data
		usersQuery := `{ "query": "{ users { id email name } }" }`
		_, usersBody := doGraphQLRequest(t, gqlPort, usersQuery)
		usersData := parseGraphQLResponse(t, usersBody)
		users := usersData["users"].([]interface{})
		if len(users) == 0 {
			t.Skip("no users in database to query")
		}
		firstUser := users[0].(map[string]interface{})
		firstUserID := firstUser["id"]
		expectedEmail := firstUser["email"]
		expectedName := firstUser["name"]

		// For HCL-first without SDL arguments, we use input argument
		query := fmt.Sprintf(`{ "query": "{ user(input: {id: %v}) { id email name } }" }`, firstUserID)
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		user := data["user"]

		if user == nil {
			t.Fatal("expected user data")
		}

		userObj := user.(map[string]interface{})

		// Verify data matches
		if userObj["id"] != firstUserID {
			t.Errorf("expected id '%v', got '%v'", firstUserID, userObj["id"])
		}
		if userObj["email"] != expectedEmail {
			t.Errorf("expected email '%v', got '%v'", expectedEmail, userObj["email"])
		}
		if userObj["name"] != expectedName {
			t.Errorf("expected name '%v', got '%v'", expectedName, userObj["name"])
		}
	})

	t.Run("Query.user returns null for non-existent ID", func(t *testing.T) {
		query := `{ "query": "{ user(input: {id: 99999}) { id email } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		user := data["user"]

		if user != nil {
			t.Errorf("expected null for non-existent user, got %v", user)
		}
	})

	// =========================================================================
	// CREATE Operations
	// =========================================================================

	t.Run("Mutation.createUser with transforms returns User type", func(t *testing.T) {
		query := `{ "query": "mutation { createUser(input: { email: \"HCLCREATE@TEST.COM\", name: \"  HCL Created  \" }) { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUser"].(map[string]interface{})

		// Verify transforms applied
		if created["email"] != "hclcreate@test.com" {
			t.Errorf("expected email 'hclcreate@test.com', got '%v'", created["email"])
		}
		if created["name"] != "HCL Created" {
			t.Errorf("expected name 'HCL Created', got '%v'", created["name"])
		}

		// Verify ID from HCL type
		if created["id"] == nil {
			t.Error("expected 'id' field from HCL type")
		}

		// Verify in database
		var dbEmail, dbName string
		err := db.QueryRow("SELECT email, name FROM users ORDER BY id DESC LIMIT 1").Scan(&dbEmail, &dbName)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		if dbEmail != "hclcreate@test.com" {
			t.Errorf("expected DB email 'hclcreate@test.com', got '%s'", dbEmail)
		}
	})

	t.Run("Mutation.createUserWithUUID generates UUID and returns externalId", func(t *testing.T) {
		query := `{ "query": "mutation { createUserWithUUID(input: { email: \"hcluuid@test.com\", name: \"HCL UUID\" }) { id email externalId } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUserWithUUID"].(map[string]interface{})

		// Verify ID returned
		if created["id"] == nil {
			t.Error("expected 'id' in response")
		}

		// Verify externalId is returned (snake_case -> camelCase mapping)
		externalId, ok := created["externalId"].(string)
		if !ok || externalId == "" {
			t.Errorf("expected 'externalId' as string in response, got '%v'", created["externalId"])
		}

		// Verify UUID format (36 chars with dashes)
		if len(externalId) != 36 {
			t.Errorf("expected UUID (36 chars), got '%s' (%d chars)", externalId, len(externalId))
		}

		// Verify UUID in database matches
		var dbExternalID string
		err := db.QueryRow("SELECT external_id FROM users ORDER BY id DESC LIMIT 1").Scan(&dbExternalID)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}
		if dbExternalID != externalId {
			t.Errorf("GraphQL externalId '%s' doesn't match DB external_id '%s'", externalId, dbExternalID)
		}
	})

	// =========================================================================
	// UPDATE Operations
	// =========================================================================

	t.Run("Mutation.updateUser updates user data", func(t *testing.T) {
		// First create a user to update
		createQuery := `{ "query": "mutation { createUser(input: { email: \"hclupdate@test.com\", name: \"Original HCL\" }) { id } }" }`
		resp, createBody := doGraphQLRequest(t, gqlPort, createQuery)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create failed: %s", createBody)
		}
		createData := parseGraphQLResponse(t, createBody)
		userID := createData["createUser"].(map[string]interface{})["id"]

		// Get the actual DB ID (last inserted)
		var dbID int64
		err := db.QueryRow("SELECT id FROM users ORDER BY id DESC LIMIT 1").Scan(&dbID)
		if err != nil {
			t.Fatalf("failed to get db id: %v", err)
		}

		// Update the user (HCL-first uses input argument) - use DB ID to ensure correct type
		updateQuery := fmt.Sprintf(`{ "query": "mutation { updateUser(input: {id: %d, name: \"Updated HCL\"}) }" }`, dbID)
		resp, body := doGraphQLRequest(t, gqlPort, updateQuery)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Check if there were GraphQL errors
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if errs, ok := result["errors"]; ok && errs != nil {
			t.Logf("GraphQL errors on update: %v", errs)
		}
		if data, ok := result["data"]; ok {
			t.Logf("GraphQL update response data: %v", data)
		}

		// Verify in database
		var dbName string
		err = db.QueryRow("SELECT name FROM users WHERE id = ?", dbID).Scan(&dbName)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}
		if dbName != "Updated HCL" {
			t.Errorf("expected name 'Updated HCL' in DB, got '%s' (userID from GraphQL: %v, dbID: %d)", dbName, userID, dbID)
		}
	})

	// =========================================================================
	// DELETE Operations
	// =========================================================================

	t.Run("Mutation.deleteUser deletes and returns result", func(t *testing.T) {
		// Create user to delete
		createQuery := `{ "query": "mutation { createUser(input: { email: \"hcldelete@test.com\", name: \"HCL Delete\" }) { id } }" }`
		_, createBody := doGraphQLRequest(t, gqlPort, createQuery)
		createData := parseGraphQLResponse(t, createBody)
		userID := createData["createUser"].(map[string]interface{})["id"]

		var countBefore int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&countBefore)

		// Delete
		deleteQuery := fmt.Sprintf(`{ "query": "mutation { deleteUser(input: {id: %v}) }" }`, userID)
		resp, body := doGraphQLRequest(t, gqlPort, deleteQuery)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var countAfter int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&countAfter)

		if countAfter >= countBefore {
			t.Errorf("expected count to decrease, before: %d, after: %d", countBefore, countAfter)
		}
	})

	// =========================================================================
	// Schema Introspection
	// =========================================================================

	t.Run("Introspection returns HCL-generated User type", func(t *testing.T) {
		query := `{ "query": "{ __type(name: \"User\") { name fields { name } } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		typeInfo := data["__type"]

		if typeInfo == nil {
			t.Fatal("expected User type generated from HCL")
		}

		userType := typeInfo.(map[string]interface{})
		if userType["name"] != "User" {
			t.Errorf("expected type name 'User', got '%v'", userType["name"])
		}

		// Verify fields from HCL type
		fields := userType["fields"].([]interface{})
		fieldNames := make(map[string]bool)
		for _, f := range fields {
			field := f.(map[string]interface{})
			fieldNames[field["name"].(string)] = true
		}

		if !fieldNames["id"] {
			t.Error("expected 'id' field from HCL type")
		}
		if !fieldNames["email"] {
			t.Error("expected 'email' field from HCL type")
		}
		if !fieldNames["name"] {
			t.Error("expected 'name' field from HCL type")
		}
	})

	t.Run("Playground is accessible", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/playground", gqlPort))
		if err != nil {
			t.Fatalf("failed to access playground: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for playground, got %d", resp.StatusCode)
		}
	})

	// =========================================================================
	// GraphQL Variables Tests
	// =========================================================================

	t.Run("Mutation with variables for simple types works", func(t *testing.T) {
		// In HCL-first mode, the input argument is JSON scalar
		// Variables are useful for passing the input object
		query := `{
			"query": "mutation CreateUser($input: JSON) { createUser(input: $input) { id email name } }",
			"variables": { "input": { "email": "HCLVARQUERY@TEST.COM", "name": "  HCL Var Query  " } }
		}`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUser"].(map[string]interface{})

		// Verify transforms applied via variables
		if created["email"] != "hclvarquery@test.com" {
			t.Errorf("expected email 'hclvarquery@test.com', got '%v'", created["email"])
		}
	})

	t.Run("Mutation with variables works correctly", func(t *testing.T) {
		query := `{
			"query": "mutation CreateUser($input: JSON) { createUser(input: $input) { id email name } }",
			"variables": { "input": { "email": "HCLVAR@TEST.COM", "name": "  HCL Variable  " } }
		}`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		data := parseGraphQLResponse(t, body)
		created := data["createUser"].(map[string]interface{})

		// Verify transforms applied via variables
		if created["email"] != "hclvar@test.com" {
			t.Errorf("expected email 'hclvar@test.com', got '%v'", created["email"])
		}
		if created["name"] != "HCL Variable" {
			t.Errorf("expected name 'HCL Variable', got '%v'", created["name"])
		}
	})

	// =========================================================================
	// Error Handling Tests
	// =========================================================================

	t.Run("Invalid query returns GraphQL error", func(t *testing.T) {
		query := `{ "query": "{ invalidHCLField }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for GraphQL error, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		errors, hasErrors := result["errors"]
		if !hasErrors || errors == nil {
			t.Error("expected GraphQL errors for invalid query")
		}
	})

	t.Run("Empty query returns error", func(t *testing.T) {
		query := `{ "query": "" }`
		resp, _ := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for empty query, got %d", resp.StatusCode)
		}
	})
}

func createGraphQLHCLFirstFullConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "types"), 0755)

	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "graphql-hcl-crud-test"
  version = "1.0.0"
}
`)

	// HCL Type definitions
	writeFile(t, filepath.Join(tmpDir, "types", "user.hcl"), `
type "User" {
  id         = id
  email      = string({ format = "email" })
  name       = string
  externalId = string
  createdAt  = string
}

type "CreateUserInput" {
  email = string({ format = "email" })
  name  = string
}

type "UpdateUserInput" {
  email = string
  name  = string
}
`)

	writeFile(t, filepath.Join(tmpDir, "connectors", "graphql.hcl"), fmt.Sprintf(`
connector "gql" {
  type   = "graphql"
  driver = "server"
  port       = %d
  endpoint   = "/graphql"
  playground = true
  schema {
    auto_generate = true
  }
}
`, port))

	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Flows with 'returns' attribute
	writeFile(t, filepath.Join(tmpDir, "flows", "graphql.hcl"), `
# READ: Get all users
flow "get_users" {
  from {
    connector = "gql"
    operation = "Query.users"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
  returns = "User[]"
}

# READ: Get single user
flow "get_user" {
  from {
    connector = "gql"
    operation = "Query.user"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
  returns = "User"
}

# CREATE: Create user with transforms
flow "create_user" {
  from {
    connector = "gql"
    operation = "Mutation.createUser"
  }
  transform {
    email = "lower(input.email)"
    name  = "trim(input.name)"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
  returns = "User"
}

# CREATE: Create user with UUID
flow "create_user_uuid" {
  from {
    connector = "gql"
    operation = "Mutation.createUserWithUUID"
  }
  transform {
    email       = "input.email"
    name        = "input.name"
    external_id = "uuid()"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
  returns = "User"
}

# UPDATE: Update user (returns affected count, not User)
flow "update_user" {
  from {
    connector = "gql"
    operation = "Mutation.updateUser"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
  returns = "JSON"
}

# DELETE: Delete user
flow "delete_user" {
  from {
    connector = "gql"
    operation = "Mutation.deleteUser"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
  returns = "Boolean"
}
`)
}

// TestIntegration_RawSQL tests raw SQL query support in flows.
func TestIntegration_RawSQL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-rawsql-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupRawSQLTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	port := 3950
	createRawSQLTestConfig(t, tmpDir, dbPath, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForServer(t, port)

	// Test: Raw SQL SELECT with JOIN
	t.Run("GET with raw SQL JOIN query", func(t *testing.T) {
		resp, body := doRequest(t, "GET", fmt.Sprintf("http://localhost:%d/orders/1", port), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var results []map[string]interface{}
		if err := json.Unmarshal([]byte(body), &results); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		order := results[0]
		// Verify JOIN worked - should have user_name from users table
		if order["user_name"] == nil {
			t.Error("expected user_name from JOIN")
		}
		if order["user_name"] != "John Doe" {
			t.Errorf("expected user_name 'John Doe', got '%v'", order["user_name"])
		}
	})

	// Test: Raw SQL with multiple parameters
	t.Run("GET with raw SQL and multiple named params", func(t *testing.T) {
		resp, body := doRequest(t, "GET", fmt.Sprintf("http://localhost:%d/orders-by-user/1", port), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var results []map[string]interface{}
		if err := json.Unmarshal([]byte(body), &results); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		// Should return orders for user 1
		if len(results) < 1 {
			t.Fatalf("expected at least 1 order for user 1, got %d", len(results))
		}
	})

	// Test: Raw SQL INSERT (alternative syntax)
	t.Run("POST with raw SQL insert", func(t *testing.T) {
		payload := map[string]interface{}{
			"user_id": 2,
			"product": "Keyboard",
			"amount":  79.99,
		}

		resp, body := doRequest(t, "POST", fmt.Sprintf("http://localhost:%d/orders-raw", port), payload)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify the order was created
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM orders WHERE product = 'Keyboard'").Scan(&count)
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 order with 'Keyboard', got %d", count)
		}
	})
}

func setupRawSQLTestDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Create users table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Create orders table with foreign key
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			product TEXT NOT NULL,
			amount REAL NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Insert test data
	_, err = db.Exec(`
		INSERT INTO users (email, name) VALUES
			('john@example.com', 'John Doe'),
			('jane@example.com', 'Jane Smith')
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	_, err = db.Exec(`
		INSERT INTO orders (user_id, product, amount) VALUES
			(1, 'Laptop', 999.99),
			(1, 'Mouse', 29.99),
			(2, 'Monitor', 349.99)
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func createRawSQLTestConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)

	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "rawsql-test"
  version = "1.0.0"
}
`)

	writeFile(t, filepath.Join(tmpDir, "connectors", "api.hcl"), fmt.Sprintf(`
connector "api" {
  type = "rest"
  port = %d
}
`, port))

	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Flows with raw SQL queries
	writeFile(t, filepath.Join(tmpDir, "flows", "orders.hcl"), `
# GET order with JOIN - using raw SQL
flow "get_order_with_user" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  to {
    connector = "sqlite"
    query     = <<-SQL
      SELECT o.*, u.name as user_name, u.email as user_email
      FROM orders o
      JOIN users u ON u.id = o.user_id
      WHERE o.id = :id
    SQL
  }
}

# GET orders by user - multiple params
flow "get_orders_by_user" {
  from {
    connector = "api"
    operation = "GET /orders-by-user/:user_id"
  }

  to {
    connector = "sqlite"
    query     = "SELECT * FROM orders WHERE user_id = :user_id ORDER BY created_at DESC"
  }
}

# POST with raw SQL insert
flow "create_order_raw" {
  from {
    connector = "api"
    operation = "POST /orders-raw"
  }

  to {
    connector = "sqlite"
    query     = "INSERT INTO orders (user_id, product, amount) VALUES (:user_id, :product, :amount)"
  }
}
`)
}

// TestIntegration_Aspects tests aspect-oriented programming features.
func TestIntegration_Aspects(t *testing.T) {
	// Create temp directory for test config
	tmpDir, err := os.MkdirTemp("", "mycel-aspect-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create flows directory (needed for pattern matching)
	flowsDir := filepath.Join(tmpDir, "flows", "products")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatalf("failed to create flows dir: %v", err)
	}

	// Setup SQLite databases
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := setupTestDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to setup database: %v", err)
	}
	defer db.Close()

	// Create audit_logs table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		flow_name TEXT,
		operation TEXT,
		target TEXT,
		timestamp INTEGER
	)`)
	if err != nil {
		t.Fatalf("failed to create audit_logs table: %v", err)
	}

	// Create products table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS products (
		id TEXT PRIMARY KEY,
		name TEXT,
		price REAL,
		created_at INTEGER
	)`)
	if err != nil {
		t.Fatalf("failed to create products table: %v", err)
	}

	port := 3980

	// Create config files
	writeFile(t, filepath.Join(tmpDir, "config.hcl"), fmt.Sprintf(`
service {
  name    = "aspect-test"
  version = "1.0.0"
}

connector "api" {
  type = "rest"
  port = %d
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, port, dbPath))

	// Create flows in the flows/products directory for pattern matching
	// Each flow in its own file so aspects can match by file name
	writeFile(t, filepath.Join(flowsDir, "get_products.hcl"), `
flow "get_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
`)

	writeFile(t, filepath.Join(flowsDir, "create_product.hcl"), `
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }
  transform {
    id         = "uuid()"
    created_at = "now()"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
`)

	// Create aspects file
	writeFile(t, filepath.Join(tmpDir, "aspects.hcl"), `
# After aspect - logs all create operations
aspect "audit_creates" {
  on   = ["**/create_*.hcl"]
  when = "after"
  if   = "result.affected > 0"

  action {
    connector = "db"
    target    = "audit_logs"
    transform {
      id         = "uuid()"
      flow_name  = "_flow"
      operation  = "_operation"
      target     = "_target"
      timestamp  = "_timestamp"
    }
  }
}
`)

	// Start the runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	// Wait for server to be ready
	time.Sleep(200 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	// Test 1: Verify aspects are registered
	t.Run("aspects_registered", func(t *testing.T) {
		if rt.aspectRegistry == nil {
			t.Error("aspect registry should not be nil")
			return
		}
		count := rt.aspectRegistry.Count()
		if count != 1 {
			t.Errorf("expected 1 aspect registered, got %d", count)
		}
	})

	// Test 2: Create a product and verify audit log was created
	t.Run("after_aspect_executed", func(t *testing.T) {
		// Create a product
		productData := map[string]interface{}{
			"name":  "Test Product",
			"price": 19.99,
		}
		body, _ := json.Marshal(productData)

		resp, err := http.Post(baseURL+"/products", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /products failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}

		// Give aspect time to execute
		time.Sleep(100 * time.Millisecond)

		// Check that audit log was created
		var auditCount int
		err = db.QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&auditCount)
		if err != nil {
			t.Fatalf("failed to query audit_logs: %v", err)
		}

		if auditCount == 0 {
			t.Error("expected audit log entry to be created by after aspect")
		}
	})

	// Test 3: Verify aspect matching works
	t.Run("aspect_matching", func(t *testing.T) {
		// The audit_creates aspect should match flows/products/create_product.hcl
		// but not flows/products/get_products.hcl
		matches := rt.aspectRegistry.Match("flows/products/create_product.hcl")
		if len(matches) == 0 {
			t.Error("expected audit_creates aspect to match create_product flow")
		}

		// get_products should not match
		getMatches := rt.aspectRegistry.Match("flows/products/get_products.hcl")
		for _, m := range getMatches {
			if m.Name == "audit_creates" {
				t.Error("audit_creates should not match get_products flow")
			}
		}
	})
}

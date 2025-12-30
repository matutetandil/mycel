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

// TestIntegration_GraphQL tests GraphQL server with SQLite database.
func TestIntegration_GraphQL(t *testing.T) {
	// Create temp directory for test config
	tmpDir, err := os.MkdirTemp("", "mycel-graphql-test-*")
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

	// Insert test data
	_, err = db.Exec("INSERT INTO users (email, name) VALUES ('john@example.com', 'John Doe'), ('jane@example.com', 'Jane Doe')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Create GraphQL test configuration
	gqlPort := 4901
	createGraphQLTestConfig(t, tmpDir, dbPath, gqlPort)

	// Start the runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	// Wait for GraphQL server to be ready
	waitForGraphQLServer(t, gqlPort)

	// Test: Query all users
	// Note: The generic GraphQL schema returns [JSON], so we query without sub-selection
	t.Run("Query.users returns all users", func(t *testing.T) {
		query := `{ "query": "{ users }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		// Check for errors
		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		// Check data
		data, ok := result["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected data in response, got: %s", body)
		}

		users, ok := data["users"].([]interface{})
		if !ok {
			t.Fatalf("expected users array in data, got: %v", data)
		}

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}
	})

	// Test: Query single user by ID
	// Uses the generic "input" argument that accepts JSON
	t.Run("Query.user returns single user", func(t *testing.T) {
		query := `{ "query": "{ user(input: {id: 1}) }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		data, ok := result["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected data in response")
		}

		// The result is an array of JSON values
		users, ok := data["user"].([]interface{})
		if !ok || len(users) == 0 {
			t.Fatalf("expected user array in data, got: %v", data)
		}

		user, ok := users[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected user object, got: %v", users[0])
		}

		if user["email"] != "john@example.com" {
			t.Errorf("expected email 'john@example.com', got '%v'", user["email"])
		}
	})

	// Test: Create user mutation
	t.Run("Mutation.createUser creates a new user", func(t *testing.T) {
		query := `{ "query": "mutation { createUser(input: { email: \"NEW@EXAMPLE.COM\", name: \"  New User  \" }) }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		// Verify the user was created in the database with transforms applied
		var email, name string
		err := db.QueryRow("SELECT email, name FROM users ORDER BY id DESC LIMIT 1").Scan(&email, &name)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		// Email should be lowercased by transform
		if email != "new@example.com" {
			t.Errorf("expected email to be lowercased 'new@example.com', got '%s'", email)
		}

		// Name should be trimmed by transform
		if name != "New User" {
			t.Errorf("expected name to be trimmed 'New User', got '%s'", name)
		}
	})

	// Test: Delete user mutation
	t.Run("Mutation.deleteUser deletes a user", func(t *testing.T) {
		// First, count users
		var countBefore int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&countBefore)

		query := `{ "query": "mutation { deleteUser(input: {id: 1}) }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		// Verify user was deleted
		var countAfter int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&countAfter)

		if countAfter >= countBefore {
			t.Errorf("expected user count to decrease, before: %d, after: %d", countBefore, countAfter)
		}
	})

	// Test: GraphQL Playground is accessible
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

func createGraphQLTestConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)

	// Main config
	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "graphql-test"
  version = "1.0.0"
}
`)

	// GraphQL connector
	writeFile(t, filepath.Join(tmpDir, "connectors", "graphql.hcl"), fmt.Sprintf(`
connector "gql" {
  type   = "graphql"
  driver = "server"

  port       = %d
  endpoint   = "/graphql"
  playground = true
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

	// GraphQL flows
	writeFile(t, filepath.Join(tmpDir, "flows", "graphql.hcl"), `
# Query: Get all users
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

# Query: Get single user by ID
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

# Mutation: Create user with transforms
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

# Mutation: Delete user
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

func waitForGraphQLServer(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		// Try to access the GraphQL health endpoint
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

// TestIntegration_GraphQL_SchemaFirst tests GraphQL with SDL schema-first approach.
// In this mode, types are defined in a .graphql SDL file and flows connect automatically.
func TestIntegration_GraphQL_SchemaFirst(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-graphql-sdl-test-*")
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

	gqlPort := 4902
	createGraphQLSchemaFirstConfig(t, tmpDir, dbPath, gqlPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForGraphQLServer(t, gqlPort)

	// Test: Query with proper typed fields from SDL
	t.Run("Query.users returns typed User objects", func(t *testing.T) {
		query := `{ "query": "{ users { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		data, ok := result["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected data in response, got: %s", body)
		}

		users, ok := data["users"].([]interface{})
		if !ok {
			t.Fatalf("expected users array, got: %v", data)
		}

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}

		// Verify the first user has typed fields
		user, ok := users[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected user object, got: %v", users[0])
		}

		// Check that typed fields are present
		if _, ok := user["id"]; !ok {
			t.Error("expected 'id' field in User type")
		}
		if _, ok := user["email"]; !ok {
			t.Error("expected 'email' field in User type")
		}
		if _, ok := user["name"]; !ok {
			t.Error("expected 'name' field in User type")
		}
	})

	// Test: Query single user with argument
	// Note: user query returns [User] because handler returns []map from database
	t.Run("Query.user with id argument returns user array", func(t *testing.T) {
		query := `{ "query": "{ user(id: 1) { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		data := result["data"].(map[string]interface{})
		users := data["user"]

		// Returns array with filtered results
		if users == nil {
			t.Fatal("expected user data")
		}

		// Should be an array
		usersArr, ok := users.([]interface{})
		if !ok {
			t.Fatalf("expected array, got %T", users)
		}

		if len(usersArr) == 0 {
			t.Fatal("expected at least one user")
		}
	})

	// Test: Mutation with typed input
	// Note: Mutation returns JSON (not User) because handler returns {id, affected}
	t.Run("Mutation.createUser with CreateUserInput creates user", func(t *testing.T) {
		// Don't request fields since return type is JSON scalar
		query := `{ "query": "mutation { createUser(input: { email: \"SCHEMA@TEST.COM\", name: \"  Schema User  \" }) }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		// Verify in database
		var email, name string
		err := db.QueryRow("SELECT email, name FROM users ORDER BY id DESC LIMIT 1").Scan(&email, &name)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		// Email should be lowercased by transform
		if email != "schema@test.com" {
			t.Errorf("expected email 'schema@test.com', got '%s'", email)
		}

		// Name should be trimmed by transform
		if name != "Schema User" {
			t.Errorf("expected name 'Schema User', got '%s'", name)
		}
	})

	// Test: Introspection shows our schema types
	t.Run("Introspection returns User type", func(t *testing.T) {
		query := `{ "query": "{ __type(name: \"User\") { name fields { name type { name } } } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		data := result["data"].(map[string]interface{})
		typeInfo := data["__type"]

		if typeInfo == nil {
			t.Fatal("expected User type in schema")
		}

		userType := typeInfo.(map[string]interface{})
		if userType["name"] != "User" {
			t.Errorf("expected type name 'User', got '%v'", userType["name"])
		}

		fields := userType["fields"].([]interface{})
		if len(fields) < 3 {
			t.Errorf("expected at least 3 fields (id, email, name), got %d", len(fields))
		}
	})
}

func createGraphQLSchemaFirstConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "schema"), 0755)

	// Main config
	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "graphql-sdl-test"
  version = "1.0.0"
}
`)

	// SDL Schema file
	// Note: user query returns [User] because the handler returns []map from database
	// Note: mutations return JSON because handlers return {id, affected} not the created object
	writeFile(t, filepath.Join(tmpDir, "schema", "schema.graphql"), `
type User {
  id: ID!
  email: String!
  name: String!
  external_id: String
  created_at: String
}

input CreateUserInput {
  email: String!
  name: String!
}

input DeleteUserInput {
  id: ID!
}

scalar JSON

type Query {
  users: [User!]!
  user(id: ID!): [User]
}

type Mutation {
  createUser(input: CreateUserInput!): JSON
  deleteUser(input: DeleteUserInput!): JSON
}
`)

	// GraphQL connector with schema path
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

	// SQLite connector
	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Flows connect to schema types
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

// TestIntegration_GraphQL_HCLFirst tests GraphQL with HCL-first approach.
// In this mode, types are defined in HCL and flows use the 'returns' attribute.
//
// TODO: This test is skipped because HCL-first mode requires runtime changes:
// 1. Runtime needs to pass HCL types to the GraphQL connector
// 2. Flow's 'returns' attribute needs to be used when registering handlers
// 3. The connector needs to convert HCL types to GraphQL types
//
// For now, users should use Schema-first mode with an SDL file.
func TestIntegration_GraphQL_HCLFirst(t *testing.T) {
	t.Skip("HCL-first mode requires runtime integration to pass types to GraphQL connector")

	tmpDir, err := os.MkdirTemp("", "mycel-graphql-hcl-test-*")
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

	gqlPort := 4903
	createGraphQLHCLFirstConfig(t, tmpDir, dbPath, gqlPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := startTestRuntime(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}
	defer rt.Shutdown()

	waitForGraphQLServer(t, gqlPort)

	// Test: Query returns typed results from HCL types
	t.Run("Query.users returns User[] type from HCL", func(t *testing.T) {
		query := `{ "query": "{ users { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		data, ok := result["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected data in response")
		}

		users, ok := data["users"].([]interface{})
		if !ok {
			t.Fatalf("expected users array")
		}

		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}

		// Verify typed fields
		user := users[0].(map[string]interface{})
		if _, ok := user["id"]; !ok {
			t.Error("expected 'id' field from HCL type")
		}
		if _, ok := user["email"]; !ok {
			t.Error("expected 'email' field from HCL type")
		}
	})

	// Test: Mutation with HCL-first types
	t.Run("Mutation.createUser uses returns type", func(t *testing.T) {
		query := `{ "query": "mutation { createUser(input: { email: \"HCL@TEST.COM\", name: \"  HCL User  \" }) { id email name } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		// Verify in database
		var email, name string
		err := db.QueryRow("SELECT email, name FROM users ORDER BY id DESC LIMIT 1").Scan(&email, &name)
		if err != nil {
			t.Fatalf("failed to query database: %v", err)
		}

		// Email should be lowercased
		if email != "hcl@test.com" {
			t.Errorf("expected email 'hcl@test.com', got '%s'", email)
		}

		// Name should be trimmed
		if name != "HCL User" {
			t.Errorf("expected name 'HCL User', got '%s'", name)
		}
	})

	// Test: Introspection shows HCL-generated types
	t.Run("Introspection returns HCL-generated User type", func(t *testing.T) {
		query := `{ "query": "{ __type(name: \"User\") { name fields { name } } }" }`
		resp, body := doGraphQLRequest(t, gqlPort, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if errs, ok := result["errors"]; ok {
			t.Fatalf("GraphQL errors: %v", errs)
		}

		data := result["data"].(map[string]interface{})
		typeInfo := data["__type"]

		if typeInfo == nil {
			t.Fatal("expected User type generated from HCL")
		}

		userType := typeInfo.(map[string]interface{})
		if userType["name"] != "User" {
			t.Errorf("expected type name 'User', got '%v'", userType["name"])
		}
	})
}

func createGraphQLHCLFirstConfig(t *testing.T, tmpDir, dbPath string, port int) {
	t.Helper()

	os.MkdirAll(filepath.Join(tmpDir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "flows"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "types"), 0755)

	// Main config
	writeFile(t, filepath.Join(tmpDir, "config.hcl"), `
service {
  name    = "graphql-hcl-test"
  version = "1.0.0"
}
`)

	// HCL Type definitions
	writeFile(t, filepath.Join(tmpDir, "types", "user.hcl"), `
type "User" {
  id         = id
  email      = string({ format = "email" })
  name       = string
  external_id = string
  created_at = string
}

type "CreateUserInput" {
  email = string({ format = "email" })
  name  = string
}
`)

	// GraphQL connector with auto_generate
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

	// SQLite connector
	writeFile(t, filepath.Join(tmpDir, "connectors", "database.hcl"), fmt.Sprintf(`
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "%s"
}
`, dbPath))

	// Flows with 'returns' attribute for HCL-first mode
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

  returns = "User[]"
}

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

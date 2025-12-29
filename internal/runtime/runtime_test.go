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

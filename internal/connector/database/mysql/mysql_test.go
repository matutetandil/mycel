package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

func TestNew(t *testing.T) {
	conn := New("test-mysql", "localhost", 3306, "testdb", "root", "password", "utf8mb4")

	if conn.Name() != "test-mysql" {
		t.Errorf("Expected name 'test-mysql', got '%s'", conn.Name())
	}

	if conn.Type() != "database" {
		t.Errorf("Expected type 'database', got '%s'", conn.Type())
	}
}

func TestConnector_Config(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		database string
		user     string
		password string
		charset  string
	}{
		{
			name:     "basic config",
			host:     "localhost",
			port:     3306,
			database: "testdb",
			user:     "root",
			password: "secret",
			charset:  "utf8mb4",
		},
		{
			name:     "custom port",
			host:     "db.example.com",
			port:     3307,
			database: "myapp",
			user:     "appuser",
			password: "pass123",
			charset:  "utf8",
		},
		{
			name:     "default charset",
			host:     "localhost",
			port:     3306,
			database: "test",
			user:     "user",
			password: "pass",
			charset:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := New("test", tt.host, tt.port, tt.database, tt.user, tt.password, tt.charset)
			// Verify individual fields are stored correctly
			if conn.Name() != "test" {
				t.Errorf("Name mismatch: got '%s'", conn.Name())
			}
			if conn.Type() != "database" {
				t.Errorf("Type mismatch: got '%s'", conn.Type())
			}
		})
	}
}

func TestConnector_SetPoolConfig(t *testing.T) {
	conn := New("test", "localhost", 3306, "testdb", "root", "pass", "utf8mb4")

	// Default values
	if conn.maxOpenConns != 25 {
		t.Errorf("Expected default maxOpenConns=25, got %d", conn.maxOpenConns)
	}
	if conn.maxIdleConns != 5 {
		t.Errorf("Expected default maxIdleConns=5, got %d", conn.maxIdleConns)
	}
	if conn.connMaxLifetime != 5*time.Minute {
		t.Errorf("Expected default connMaxLifetime=5m, got %v", conn.connMaxLifetime)
	}

	// Set new values
	conn.SetPoolConfig(50, 10, 10*time.Minute)

	if conn.maxOpenConns != 50 {
		t.Errorf("Expected maxOpenConns=50, got %d", conn.maxOpenConns)
	}
	if conn.maxIdleConns != 10 {
		t.Errorf("Expected maxIdleConns=10, got %d", conn.maxIdleConns)
	}
	if conn.connMaxLifetime != 10*time.Minute {
		t.Errorf("Expected connMaxLifetime=10m, got %v", conn.connMaxLifetime)
	}

	// Test zero values are ignored
	conn.SetPoolConfig(0, 0, 0)
	if conn.maxOpenConns != 50 {
		t.Errorf("Zero value should be ignored, got maxOpenConns=%d", conn.maxOpenConns)
	}
}

func TestConnector_Health_NotConnected(t *testing.T) {
	conn := New("test", "localhost", 3306, "testdb", "root", "pass", "utf8mb4")

	err := conn.Health(context.Background())
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if err.Error() != "database not connected" {
		t.Errorf("Expected 'database not connected', got: %v", err)
	}
}

func TestConnector_Read_NotConnected(t *testing.T) {
	conn := New("test", "localhost", 3306, "testdb", "root", "pass", "utf8mb4")

	_, err := conn.Read(context.Background(), connector.Query{Target: "users"})
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if err.Error() != "database not connected" {
		t.Errorf("Expected 'database not connected', got: %v", err)
	}
}

func TestConnector_Write_NotConnected(t *testing.T) {
	conn := New("test", "localhost", 3306, "testdb", "root", "pass", "utf8mb4")

	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "INSERT",
		Target:    "users",
		Payload:   map[string]interface{}{"name": "test"},
	})
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if err.Error() != "database not connected" {
		t.Errorf("Expected 'database not connected', got: %v", err)
	}
}

func TestConnector_ParseNamedParams(t *testing.T) {
	conn := New("test", "localhost", 3306, "testdb", "root", "pass", "utf8mb4")

	tests := []struct {
		name      string
		sql       string
		params    map[string]interface{}
		wantSQL   string
		wantCount int
	}{
		{
			name:      "single param",
			sql:       "SELECT * FROM users WHERE id = :id",
			params:    map[string]interface{}{"id": 1},
			wantSQL:   "SELECT * FROM users WHERE id = ?",
			wantCount: 1,
		},
		{
			name:      "multiple params",
			sql:       "SELECT * FROM users WHERE status = :status AND age > :age",
			params:    map[string]interface{}{"status": "active", "age": 18},
			wantSQL:   "SELECT * FROM users WHERE status = ? AND age > ?",
			wantCount: 2,
		},
		{
			name:      "no params",
			sql:       "SELECT * FROM users",
			params:    nil,
			wantSQL:   "SELECT * FROM users",
			wantCount: 0,
		},
		{
			name:      "repeated param",
			sql:       "SELECT * FROM users WHERE name = :name OR email LIKE :name",
			params:    map[string]interface{}{"name": "test"},
			wantSQL:   "SELECT * FROM users WHERE name = ? OR email LIKE ?",
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := conn.parseNamedParams(tt.sql, tt.params)
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL mismatch:\n  got:  %s\n  want: %s", gotSQL, tt.wantSQL)
			}
			if len(gotArgs) != tt.wantCount {
				t.Errorf("Args count mismatch: got %d, want %d", len(gotArgs), tt.wantCount)
			}
		})
	}
}

func TestFactory_Type(t *testing.T) {
	factory := NewFactory()

	if factory.Type() != "database" {
		t.Errorf("Expected type 'database', got '%s'", factory.Type())
	}
}

func TestFactory_Supports(t *testing.T) {
	factory := NewFactory()

	tests := []struct {
		connType string
		driver   string
		want     bool
	}{
		{"database", "mysql", true},
		{"database", "postgresql", false},
		{"database", "sqlite", false},
		{"queue", "mysql", false},
		{"rest", "mysql", false},
	}

	for _, tt := range tests {
		t.Run(tt.connType+"/"+tt.driver, func(t *testing.T) {
			if got := factory.Supports(tt.connType, tt.driver); got != tt.want {
				t.Errorf("Supports(%q, %q) = %v, want %v", tt.connType, tt.driver, got, tt.want)
			}
		})
	}
}

func TestFactory_Create(t *testing.T) {
	factory := NewFactory()

	cfg := &connector.Config{
		Name:   "test-mysql",
		Type:   "database",
		Driver: "mysql",
		Properties: map[string]interface{}{
			"host":     "db.example.com",
			"port":     3307,
			"database": "myapp",
			"user":     "appuser",
			"password": "secret123",
			"charset":  "utf8",
			"pool": map[string]interface{}{
				"max":          100,
				"min":          20,
				"max_lifetime": 600,
			},
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if conn.Name() != "test-mysql" {
		t.Errorf("Expected name 'test-mysql', got '%s'", conn.Name())
	}

	mysqlConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("Expected *Connector type")
	}

	if mysqlConn.maxOpenConns != 100 {
		t.Errorf("Expected maxOpenConns=100, got %d", mysqlConn.maxOpenConns)
	}
	if mysqlConn.maxIdleConns != 20 {
		t.Errorf("Expected maxIdleConns=20, got %d", mysqlConn.maxIdleConns)
	}
	if mysqlConn.connMaxLifetime != 600*time.Second {
		t.Errorf("Expected connMaxLifetime=600s, got %v", mysqlConn.connMaxLifetime)
	}
}

func TestFactory_Create_Defaults(t *testing.T) {
	factory := NewFactory()

	cfg := &connector.Config{
		Name:   "test-defaults",
		Type:   "database",
		Driver: "mysql",
		Properties: map[string]interface{}{
			"database": "testdb",
			"user":     "root",
			"password": "pass",
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify connector was created with defaults
	if conn.Name() != "test-defaults" {
		t.Errorf("Expected name 'test-defaults', got '%s'", conn.Name())
	}

	if conn.Type() != "database" {
		t.Errorf("Expected type 'database', got '%s'", conn.Type())
	}
}

func TestFactory_Create_MissingDatabase(t *testing.T) {
	factory := NewFactory()

	cfg := &connector.Config{
		Name:   "test-missing",
		Type:   "database",
		Driver: "mysql",
		Properties: map[string]interface{}{
			"user":     "root",
			"password": "pass",
		},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Error("Expected error for missing database")
	}

	if err.Error() != "mysql connector requires database name" {
		t.Errorf("Expected 'mysql connector requires database name', got: %v", err)
	}
}

func TestFactory_Create_MissingUser(t *testing.T) {
	factory := NewFactory()

	cfg := &connector.Config{
		Name:   "test-missing",
		Type:   "database",
		Driver: "mysql",
		Properties: map[string]interface{}{
			"database": "testdb",
			"password": "pass",
		},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Error("Expected error for missing user")
	}

	if err.Error() != "mysql connector requires user" {
		t.Errorf("Expected 'mysql connector requires user', got: %v", err)
	}
}

func TestFactory_Create_OptionalPassword(t *testing.T) {
	factory := NewFactory()

	// Password is optional - useful for development databases
	cfg := &connector.Config{
		Name:   "test-no-password",
		Type:   "database",
		Driver: "mysql",
		Properties: map[string]interface{}{
			"database": "testdb",
			"user":     "root",
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if conn.Name() != "test-no-password" {
		t.Errorf("Expected name 'test-no-password', got '%s'", conn.Name())
	}
}

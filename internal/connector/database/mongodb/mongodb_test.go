package mongodb

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/mycel-labs/mycel/internal/connector"
)

func TestNew(t *testing.T) {
	conn := New("test-mongo", "mongodb://localhost:27017", "testdb")

	if conn.Name() != "test-mongo" {
		t.Errorf("Expected name 'test-mongo', got '%s'", conn.Name())
	}

	if conn.Type() != "database" {
		t.Errorf("Expected type 'database', got '%s'", conn.Type())
	}
}

func TestNew_DefaultURI(t *testing.T) {
	conn := New("test-mongo", "", "testdb")

	if conn.uri != "mongodb://localhost:27017" {
		t.Errorf("Expected default URI 'mongodb://localhost:27017', got '%s'", conn.uri)
	}
}

func TestConnector_SetPoolConfig(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	// Default values
	if conn.maxPoolSize != 100 {
		t.Errorf("Expected default maxPoolSize=100, got %d", conn.maxPoolSize)
	}
	if conn.minPoolSize != 5 {
		t.Errorf("Expected default minPoolSize=5, got %d", conn.minPoolSize)
	}
	if conn.connectTimeout != 10*time.Second {
		t.Errorf("Expected default connectTimeout=10s, got %v", conn.connectTimeout)
	}

	// Set new values
	conn.SetPoolConfig(200, 10, 30*time.Second)

	if conn.maxPoolSize != 200 {
		t.Errorf("Expected maxPoolSize=200, got %d", conn.maxPoolSize)
	}
	if conn.minPoolSize != 10 {
		t.Errorf("Expected minPoolSize=10, got %d", conn.minPoolSize)
	}
	if conn.connectTimeout != 30*time.Second {
		t.Errorf("Expected connectTimeout=30s, got %v", conn.connectTimeout)
	}

	// Test zero values are ignored
	conn.SetPoolConfig(0, 0, 0)
	if conn.maxPoolSize != 200 {
		t.Errorf("Zero value should be ignored, got maxPoolSize=%d", conn.maxPoolSize)
	}
}

func TestConnector_Health_NotConnected(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	err := conn.Health(context.Background())
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if err.Error() != "database not connected" {
		t.Errorf("Expected 'database not connected', got: %v", err)
	}
}

func TestConnector_Read_NotConnected(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	_, err := conn.Read(context.Background(), connector.Query{Target: "users"})
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if err.Error() != "database not connected" {
		t.Errorf("Expected 'database not connected', got: %v", err)
	}
}

func TestConnector_Write_NotConnected(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

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

func TestConnector_ConvertToBSON(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	tests := []struct {
		name   string
		input  map[string]interface{}
		verify func(bson.M) bool
	}{
		{
			name: "simple fields",
			input: map[string]interface{}{
				"name":   "John",
				"age":    30,
				"active": true,
			},
			verify: func(result bson.M) bool {
				return result["name"] == "John" &&
					result["age"] == 30 &&
					result["active"] == true
			},
		},
		{
			name: "nested object",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"name": "Jane",
				},
			},
			verify: func(result bson.M) bool {
				nested, ok := result["user"].(bson.M)
				return ok && nested["name"] == "Jane"
			},
		},
		{
			name: "valid ObjectID string",
			input: map[string]interface{}{
				"_id": "507f1f77bcf86cd799439011",
			},
			verify: func(result bson.M) bool {
				oid, ok := result["_id"].(primitive.ObjectID)
				return ok && oid.Hex() == "507f1f77bcf86cd799439011"
			},
		},
		{
			name: "id field converts to _id ObjectID",
			input: map[string]interface{}{
				"id": "507f1f77bcf86cd799439011",
			},
			verify: func(result bson.M) bool {
				oid, ok := result["_id"].(primitive.ObjectID)
				return ok && oid.Hex() == "507f1f77bcf86cd799439011"
			},
		},
		{
			name: "invalid ObjectID stays as string",
			input: map[string]interface{}{
				"_id": "not-a-valid-objectid",
			},
			verify: func(result bson.M) bool {
				return result["_id"] == "not-a-valid-objectid"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conn.convertToBSON(tt.input)
			if !tt.verify(result) {
				t.Errorf("BSON conversion verification failed for input: %v, result: %v", tt.input, result)
			}
		})
	}
}

func TestConnector_ConvertBSONToMap(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	oid, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	dateTime := primitive.NewDateTimeFromTime(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC))

	tests := []struct {
		name   string
		input  bson.M
		verify func(map[string]interface{}) bool
	}{
		{
			name: "simple fields",
			input: bson.M{
				"name":   "John",
				"age":    30,
				"active": true,
			},
			verify: func(result map[string]interface{}) bool {
				return result["name"] == "John" &&
					result["age"] == 30 &&
					result["active"] == true
			},
		},
		{
			name: "ObjectID to string",
			input: bson.M{
				"_id": oid,
			},
			verify: func(result map[string]interface{}) bool {
				return result["_id"] == "507f1f77bcf86cd799439011"
			},
		},
		{
			name: "DateTime to RFC3339",
			input: bson.M{
				"created_at": dateTime,
			},
			verify: func(result map[string]interface{}) bool {
				s, ok := result["created_at"].(string)
				if !ok {
					return false
				}
				// Parse the RFC3339 string and verify it represents the same time
				parsed, err := time.Parse(time.RFC3339, s)
				if err != nil {
					return false
				}
				// Compare in UTC to handle timezone differences
				return parsed.UTC().Equal(dateTime.Time().UTC())
			},
		},
		{
			name: "nested BSON.M",
			input: bson.M{
				"user": bson.M{
					"name": "Jane",
					"_id":  oid,
				},
			},
			verify: func(result map[string]interface{}) bool {
				nested, ok := result["user"].(map[string]interface{})
				return ok && nested["name"] == "Jane" && nested["_id"] == "507f1f77bcf86cd799439011"
			},
		},
		{
			name: "BSON array",
			input: bson.M{
				"tags": bson.A{"go", "mongodb", "test"},
			},
			verify: func(result map[string]interface{}) bool {
				tags, ok := result["tags"].([]interface{})
				return ok && len(tags) == 3 && tags[0] == "go"
			},
		},
		{
			name: "array with ObjectIDs",
			input: bson.M{
				"refs": bson.A{oid, oid},
			},
			verify: func(result map[string]interface{}) bool {
				refs, ok := result["refs"].([]interface{})
				return ok && len(refs) == 2 && refs[0] == "507f1f77bcf86cd799439011"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conn.convertBSONToMap(tt.input)
			if !tt.verify(result) {
				t.Errorf("Map conversion verification failed for input: %v, result: %v", tt.input, result)
			}
		})
	}
}

func TestConnector_BuildFilter(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	tests := []struct {
		name   string
		query  connector.Query
		verify func(bson.M) bool
	}{
		{
			name:  "empty query returns empty filter",
			query: connector.Query{Target: "users"},
			verify: func(result bson.M) bool {
				return len(result) == 0
			},
		},
		{
			name: "RawQuery has highest priority",
			query: connector.Query{
				Target: "users",
				RawQuery: map[string]interface{}{
					"status": "active",
				},
				Filters: map[string]interface{}{
					"role": "admin",
				},
			},
			verify: func(result bson.M) bool {
				return result["status"] == "active" && result["role"] == nil
			},
		},
		{
			name: "Filters used when no RawQuery",
			query: connector.Query{
				Target: "users",
				Filters: map[string]interface{}{
					"role":   "admin",
					"active": true,
				},
			},
			verify: func(result bson.M) bool {
				return result["role"] == "admin" && result["active"] == true
			},
		},
		{
			name: "MongoDB operators preserved",
			query: connector.Query{
				Target: "users",
				RawQuery: map[string]interface{}{
					"age": map[string]interface{}{
						"$gte": 18,
						"$lt":  65,
					},
				},
			},
			verify: func(result bson.M) bool {
				age, ok := result["age"].(bson.M)
				return ok && age["$gte"] == 18 && age["$lt"] == 65
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conn.buildFilter(tt.query)
			if !tt.verify(result) {
				t.Errorf("Filter verification failed for query: %+v, result: %v", tt.query, result)
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
		{"database", "mongodb", true},
		{"database", "mongo", true},
		{"database", "mysql", false},
		{"database", "postgresql", false},
		{"queue", "mongodb", false},
		{"rest", "mongo", false},
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
		Name:   "test-mongo",
		Type:   "database",
		Driver: "mongodb",
		Properties: map[string]interface{}{
			"uri":      "mongodb://admin:secret@db.example.com:27018",
			"database": "myapp",
			"pool": map[string]interface{}{
				"max":             200,
				"min":             10,
				"connect_timeout": 30,
			},
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if conn.Name() != "test-mongo" {
		t.Errorf("Expected name 'test-mongo', got '%s'", conn.Name())
	}

	mongoConn, ok := conn.(*Connector)
	if !ok {
		t.Fatal("Expected *Connector type")
	}

	if mongoConn.uri != "mongodb://admin:secret@db.example.com:27018" {
		t.Errorf("URI mismatch: got '%s'", mongoConn.uri)
	}

	if mongoConn.database != "myapp" {
		t.Errorf("Expected database 'myapp', got '%s'", mongoConn.database)
	}

	if mongoConn.maxPoolSize != 200 {
		t.Errorf("Expected maxPoolSize=200, got %d", mongoConn.maxPoolSize)
	}
	if mongoConn.minPoolSize != 10 {
		t.Errorf("Expected minPoolSize=10, got %d", mongoConn.minPoolSize)
	}
	if mongoConn.connectTimeout != 30*time.Second {
		t.Errorf("Expected connectTimeout=30s, got %v", mongoConn.connectTimeout)
	}
}

func TestFactory_Create_BuildURI(t *testing.T) {
	factory := NewFactory()

	tests := []struct {
		name       string
		properties map[string]interface{}
		wantURI    string
	}{
		{
			name: "with auth",
			properties: map[string]interface{}{
				"host":     "db.example.com",
				"port":     27018,
				"database": "testdb",
				"user":     "admin",
				"password": "secret",
			},
			wantURI: "mongodb://admin:secret@db.example.com:27018",
		},
		{
			name: "without auth",
			properties: map[string]interface{}{
				"host":     "localhost",
				"database": "testdb",
			},
			wantURI: "mongodb://localhost:27017",
		},
		{
			name: "defaults",
			properties: map[string]interface{}{
				"database": "testdb",
			},
			wantURI: "mongodb://localhost:27017",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &connector.Config{
				Name:       "test",
				Type:       "database",
				Driver:     "mongodb",
				Properties: tt.properties,
			}

			conn, err := factory.Create(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Create failed: %v", err)
			}

			mongoConn := conn.(*Connector)
			if mongoConn.uri != tt.wantURI {
				t.Errorf("URI mismatch:\n  got:  %s\n  want: %s", mongoConn.uri, tt.wantURI)
			}
		})
	}
}

func TestFactory_Create_MissingDatabase(t *testing.T) {
	factory := NewFactory()

	cfg := &connector.Config{
		Name:   "test-missing",
		Type:   "database",
		Driver: "mongodb",
		Properties: map[string]interface{}{
			"uri": "mongodb://localhost:27017",
		},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Error("Expected error for missing database")
	}

	if err.Error() != "mongodb connector requires database name" {
		t.Errorf("Expected 'mongodb connector requires database name', got: %v", err)
	}
}

func TestConnector_ConvertBSONArray(t *testing.T) {
	conn := New("test", "mongodb://localhost:27017", "testdb")

	oid, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	dateTime := primitive.NewDateTimeFromTime(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC))

	tests := []struct {
		name   string
		input  bson.A
		verify func([]interface{}) bool
	}{
		{
			name:  "simple strings",
			input: bson.A{"a", "b", "c"},
			verify: func(result []interface{}) bool {
				return len(result) == 3 && result[0] == "a"
			},
		},
		{
			name:  "ObjectIDs",
			input: bson.A{oid},
			verify: func(result []interface{}) bool {
				return len(result) == 1 && result[0] == "507f1f77bcf86cd799439011"
			},
		},
		{
			name:  "DateTimes",
			input: bson.A{dateTime},
			verify: func(result []interface{}) bool {
				s, ok := result[0].(string)
				if !ok {
					return false
				}
				// Parse the RFC3339 string and verify it represents the same time
				parsed, err := time.Parse(time.RFC3339, s)
				if err != nil {
					return false
				}
				// Compare in UTC to handle timezone differences
				return parsed.UTC().Equal(dateTime.Time().UTC())
			},
		},
		{
			name: "nested documents",
			input: bson.A{
				bson.M{"name": "doc1"},
				bson.M{"name": "doc2"},
			},
			verify: func(result []interface{}) bool {
				if len(result) != 2 {
					return false
				}
				doc1, ok := result[0].(map[string]interface{})
				return ok && doc1["name"] == "doc1"
			},
		},
		{
			name: "nested arrays",
			input: bson.A{
				bson.A{1, 2, 3},
				bson.A{4, 5, 6},
			},
			verify: func(result []interface{}) bool {
				if len(result) != 2 {
					return false
				}
				nested, ok := result[0].([]interface{})
				return ok && len(nested) == 3
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conn.convertBSONArray(tt.input)
			if !tt.verify(result) {
				t.Errorf("Array conversion verification failed for input: %v, result: %v", tt.input, result)
			}
		})
	}
}

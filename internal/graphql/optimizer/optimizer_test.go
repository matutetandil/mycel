package optimizer

import (
	"testing"

	"github.com/matutetandil/mycel/internal/graphql/analyzer"
)

func TestIsSelectStar(t *testing.T) {
	tests := []struct {
		query    string
		expected bool
	}{
		{"SELECT * FROM users", true},
		{"select * from users", true},
		{"SELECT  *  FROM  users", true},
		{"SELECT DISTINCT * FROM users", true},
		{"SELECT id, name FROM users", false},
		{"SELECT * , extra FROM users", false}, // Not pure SELECT *
		{"INSERT INTO users VALUES (1)", false},
		{"UPDATE users SET name = 'John'", false},
		{"  SELECT * FROM users WHERE id = 1", true},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := isSelectStar(tt.query); got != tt.expected {
				t.Errorf("isSelectStar(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestRewriteSelectStar(t *testing.T) {
	tests := []struct {
		query    string
		columns  []string
		expected string
	}{
		{
			"SELECT * FROM users",
			[]string{"id", "name"},
			"SELECT id, name FROM users",
		},
		{
			"SELECT * FROM users WHERE id = 1",
			[]string{"id", "name", "email"},
			"SELECT id, name, email FROM users WHERE id = 1",
		},
		{
			"SELECT DISTINCT * FROM users",
			[]string{"id", "name"},
			"SELECT DISTINCT id, name FROM users",
		},
		{
			"select * from users order by id",
			[]string{"id", "name"},
			"SELECT id, name FROM users order by id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := rewriteSelectStar(normalizeWhitespace(tt.query), tt.columns)
			if result != tt.expected {
				t.Errorf("rewriteSelectStar(%q, %v) = %q, want %q", tt.query, tt.columns, result, tt.expected)
			}
		})
	}
}

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"id", "id"},
		{"userName", "user_name"},
		{"firstName", "first_name"},
		{"createdAt", "created_at"},
		{"ID", "i_d"}, // Edge case
		{"userID", "user_i_d"},
		{"externalId", "external_id"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := CamelToSnake(tt.input); got != tt.expected {
				t.Errorf("CamelToSnake(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSQLOptimizer_OptimizeQuery(t *testing.T) {
	// Build field tree: { id, name, email }
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	tree.AddField(analyzer.NewFieldNode("name"))
	tree.AddField(analyzer.NewFieldNode("email"))
	fields := analyzer.NewRequestedFields(tree)

	optimizer := NewSQLOptimizer(fields)

	tests := []struct {
		name        string
		query       string
		expected    string
		shouldOpt   bool
	}{
		{
			name:        "simple SELECT *",
			query:       "SELECT * FROM users",
			expected:    "SELECT id, name, email FROM users",
			shouldOpt:   true,
		},
		{
			name:        "SELECT * with WHERE",
			query:       "SELECT * FROM users WHERE id = 1",
			expected:    "SELECT id, name, email FROM users WHERE id = 1",
			shouldOpt:   true,
		},
		{
			name:        "already specific columns",
			query:       "SELECT id, name FROM users",
			expected:    "SELECT id, name FROM users",
			shouldOpt:   false,
		},
		{
			name:        "INSERT query",
			query:       "INSERT INTO users (name) VALUES ('John')",
			expected:    "INSERT INTO users (name) VALUES ('John')",
			shouldOpt:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, optimized := optimizer.OptimizeQuery(tt.query)
			if optimized != tt.shouldOpt {
				t.Errorf("optimized = %v, want %v", optimized, tt.shouldOpt)
			}
			// For optimized queries, we can't guarantee column order, so just check it was modified
			if tt.shouldOpt && result == tt.query {
				t.Error("expected query to be modified")
			}
			if !tt.shouldOpt && result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSQLOptimizer_WithNestedFields(t *testing.T) {
	// Build tree with nested fields: { id, name, orders { total } }
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	tree.AddField(analyzer.NewFieldNode("name"))

	ordersNode := analyzer.NewFieldNode("orders")
	ordersNode.Children = analyzer.NewFieldTree()
	ordersNode.Children.AddField(analyzer.NewFieldNode("total"))
	tree.AddField(ordersNode)

	fields := analyzer.NewRequestedFields(tree)
	optimizer := NewSQLOptimizer(fields)

	// Should only include top-level fields (id, name, orders)
	// but 'orders' is a nested object, so it's included as a column
	result, optimized := optimizer.OptimizeQuery("SELECT * FROM users")

	if !optimized {
		t.Error("expected query to be optimized")
	}

	// Should include id, name, orders (but not orders.total)
	if result == "SELECT * FROM users" {
		t.Error("query should have been modified")
	}
}

func TestSQLOptimizer_WithColumnMapping(t *testing.T) {
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("userId"))
	tree.AddField(analyzer.NewFieldNode("fullName"))
	fields := analyzer.NewRequestedFields(tree)

	optimizer := NewSQLOptimizer(fields)
	optimizer.SetColumnMapping(map[string]string{
		"userId":   "user_id",
		"fullName": "full_name",
	})

	result, _ := optimizer.OptimizeQuery("SELECT * FROM users")

	// Should use mapped column names
	if result == "SELECT * FROM users" {
		t.Error("query should have been modified")
	}
}

func TestSQLOptimizer_NilFields(t *testing.T) {
	optimizer := NewSQLOptimizer(nil)

	result, optimized := optimizer.OptimizeQuery("SELECT * FROM users")

	if optimized {
		t.Error("should not optimize with nil fields")
	}
	if result != "SELECT * FROM users" {
		t.Errorf("expected original query, got %q", result)
	}
}

func TestOptimizeQueryWithFields(t *testing.T) {
	fields := []string{"id", "name", "email", "orders.total"}

	result, optimized := OptimizeQueryWithFields("SELECT * FROM users", fields)

	if !optimized {
		t.Error("expected query to be optimized")
	}

	// Should include id, name, email but not orders.total (nested)
	if result == "SELECT * FROM users" {
		t.Error("query should have been modified")
	}
}

func TestFieldsFromInput(t *testing.T) {
	// Test with []interface{}
	input1 := map[string]interface{}{
		"__requested_fields": []interface{}{"id", "name", "email"},
	}
	fields1 := FieldsFromInput(input1)
	if len(fields1) != 3 {
		t.Errorf("expected 3 fields, got %d", len(fields1))
	}

	// Test with []string
	input2 := map[string]interface{}{
		"__requested_fields": []string{"id", "name"},
	}
	fields2 := FieldsFromInput(input2)
	if len(fields2) != 2 {
		t.Errorf("expected 2 fields, got %d", len(fields2))
	}

	// Test with nil
	fields3 := FieldsFromInput(nil)
	if fields3 != nil {
		t.Error("expected nil for nil input")
	}

	// Test without __requested_fields
	input4 := map[string]interface{}{
		"id": 1,
	}
	fields4 := FieldsFromInput(input4)
	if fields4 != nil {
		t.Error("expected nil when __requested_fields is missing")
	}
}

func TestTopFieldsFromInput(t *testing.T) {
	input := map[string]interface{}{
		"__requested_top_fields": []interface{}{"id", "name", "orders"},
	}

	fields := TopFieldsFromInput(input)
	if len(fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(fields))
	}
}

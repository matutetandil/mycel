package pruner

import (
	"reflect"
	"testing"

	"github.com/matutetandil/mycel/internal/graphql/analyzer"
)

func TestPrune_SimpleMap(t *testing.T) {
	// Build tree requesting only "id" and "name"
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	tree.AddField(analyzer.NewFieldNode("name"))
	requested := analyzer.NewRequestedFields(tree)

	// Data has extra fields
	data := map[string]interface{}{
		"id":       1,
		"name":     "John",
		"email":    "john@example.com", // Should be pruned
		"password": "secret",           // Should be pruned
	}

	result := Prune(data, requested)

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected map result")
	}

	if resultMap["id"] != 1 {
		t.Errorf("Expected id=1, got %v", resultMap["id"])
	}
	if resultMap["name"] != "John" {
		t.Errorf("Expected name='John', got %v", resultMap["name"])
	}
	if _, exists := resultMap["email"]; exists {
		t.Error("email should have been pruned")
	}
	if _, exists := resultMap["password"]; exists {
		t.Error("password should have been pruned")
	}
}

func TestPrune_NestedMap(t *testing.T) {
	// Request: { id, user { name } }
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))

	userNode := analyzer.NewFieldNode("user")
	userNode.IsLeaf = false
	userNode.Children = analyzer.NewFieldTree()
	userNode.Children.AddField(analyzer.NewFieldNode("name"))
	tree.AddField(userNode)

	requested := analyzer.NewRequestedFields(tree)

	// Data has more fields at each level
	data := map[string]interface{}{
		"id":    1,
		"total": 100, // Should be pruned
		"user": map[string]interface{}{
			"name":  "John",
			"email": "john@example.com", // Should be pruned
			"age":   30,                 // Should be pruned
		},
	}

	result := Prune(data, requested)

	resultMap := result.(map[string]interface{})

	if resultMap["id"] != 1 {
		t.Errorf("Expected id=1, got %v", resultMap["id"])
	}
	if _, exists := resultMap["total"]; exists {
		t.Error("total should have been pruned")
	}

	user, ok := resultMap["user"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected user to be a map")
	}

	if user["name"] != "John" {
		t.Errorf("Expected user.name='John', got %v", user["name"])
	}
	if _, exists := user["email"]; exists {
		t.Error("user.email should have been pruned")
	}
	if _, exists := user["age"]; exists {
		t.Error("user.age should have been pruned")
	}
}

func TestPrune_Array(t *testing.T) {
	// Request: { id, name }
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	tree.AddField(analyzer.NewFieldNode("name"))
	requested := analyzer.NewRequestedFields(tree)

	// Array of objects
	data := []interface{}{
		map[string]interface{}{"id": 1, "name": "John", "email": "john@example.com"},
		map[string]interface{}{"id": 2, "name": "Jane", "email": "jane@example.com"},
	}

	result := Prune(data, requested)

	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatal("Expected slice result")
	}

	if len(resultSlice) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(resultSlice))
	}

	for i, item := range resultSlice {
		m := item.(map[string]interface{})
		if _, exists := m["email"]; exists {
			t.Errorf("Item %d: email should have been pruned", i)
		}
	}
}

func TestPrune_MapSlice(t *testing.T) {
	// Request: { id, name }
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	tree.AddField(analyzer.NewFieldNode("name"))
	requested := analyzer.NewRequestedFields(tree)

	// Typed slice of maps
	data := []map[string]interface{}{
		{"id": 1, "name": "John", "email": "john@example.com"},
		{"id": 2, "name": "Jane", "email": "jane@example.com"},
	}

	result := Prune(data, requested)

	resultSlice, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatal("Expected []map[string]interface{} result")
	}

	if len(resultSlice) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(resultSlice))
	}

	for i, item := range resultSlice {
		if _, exists := item["email"]; exists {
			t.Errorf("Item %d: email should have been pruned", i)
		}
	}
}

func TestPrune_NilData(t *testing.T) {
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	requested := analyzer.NewRequestedFields(tree)

	result := Prune(nil, requested)
	if result != nil {
		t.Error("Expected nil result for nil data")
	}
}

func TestPrune_EmptyRequested(t *testing.T) {
	data := map[string]interface{}{"id": 1, "name": "John"}

	// Empty requested fields
	result := Prune(data, analyzer.NewRequestedFields(analyzer.NewFieldTree()))

	// Should return original data unchanged
	if !reflect.DeepEqual(result, data) {
		t.Error("Expected original data when requested fields is empty")
	}
}

func TestPrune_NilRequested(t *testing.T) {
	data := map[string]interface{}{"id": 1, "name": "John"}

	// Nil requested fields
	result := Prune(data, nil)

	// Should return original data unchanged
	if !reflect.DeepEqual(result, data) {
		t.Error("Expected original data when requested fields is nil")
	}
}

func TestPruneWithPaths_Simple(t *testing.T) {
	data := map[string]interface{}{
		"id":    1,
		"name":  "John",
		"email": "john@example.com",
	}

	result := PruneWithPaths(data, []string{"id", "name"})

	resultMap := result.(map[string]interface{})
	if _, exists := resultMap["email"]; exists {
		t.Error("email should have been pruned")
	}
	if resultMap["id"] != 1 {
		t.Error("id should exist")
	}
	if resultMap["name"] != "John" {
		t.Error("name should exist")
	}
}

func TestPruneWithPaths_Nested(t *testing.T) {
	data := map[string]interface{}{
		"id": 1,
		"user": map[string]interface{}{
			"name":  "John",
			"email": "john@example.com",
		},
	}

	result := PruneWithPaths(data, []string{"id", "user.name"})

	resultMap := result.(map[string]interface{})
	if resultMap["id"] != 1 {
		t.Error("id should exist")
	}

	user := resultMap["user"].(map[string]interface{})
	if user["name"] != "John" {
		t.Error("user.name should exist")
	}
	if _, exists := user["email"]; exists {
		t.Error("user.email should have been pruned")
	}
}

func TestPruneWithPaths_DeepNested(t *testing.T) {
	data := map[string]interface{}{
		"order": map[string]interface{}{
			"id": 1,
			"items": map[string]interface{}{
				"product": map[string]interface{}{
					"name":  "Widget",
					"price": 9.99,
					"sku":   "WGT-001",
				},
			},
		},
	}

	result := PruneWithPaths(data, []string{"order.id", "order.items.product.name"})

	resultMap := result.(map[string]interface{})
	order := resultMap["order"].(map[string]interface{})

	if order["id"] != 1 {
		t.Error("order.id should exist")
	}

	items := order["items"].(map[string]interface{})
	product := items["product"].(map[string]interface{})

	if product["name"] != "Widget" {
		t.Error("order.items.product.name should exist")
	}
	if _, exists := product["price"]; exists {
		t.Error("order.items.product.price should have been pruned")
	}
	if _, exists := product["sku"]; exists {
		t.Error("order.items.product.sku should have been pruned")
	}
}

func TestBuildTreeFromPaths(t *testing.T) {
	paths := []string{"id", "name", "orders.total", "orders.items.price"}

	tree := buildTreeFromPaths(paths)
	rf := analyzer.NewRequestedFields(tree)

	tests := []struct {
		path     string
		expected bool
	}{
		{"id", true},
		{"name", true},
		{"orders", true},
		{"orders.total", true},
		{"orders.items", true},
		{"orders.items.price", true},
		{"email", false},
		{"orders.status", false},
	}

	for _, tt := range tests {
		if got := rf.Has(tt.path); got != tt.expected {
			t.Errorf("Has(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"id", []string{"id"}},
		{"user.name", []string{"user", "name"}},
		{"orders.items.price", []string{"orders", "items", "price"}},
	}

	for _, tt := range tests {
		result := splitPath(tt.path)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("splitPath(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}

	// Test empty path separately (returns nil which is equivalent to empty slice)
	emptyResult := splitPath("")
	if len(emptyResult) != 0 {
		t.Errorf("splitPath(\"\") should return empty slice, got %v", emptyResult)
	}
}

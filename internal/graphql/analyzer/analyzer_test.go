package analyzer

import (
	"sort"
	"testing"
)

func TestFieldTree_AddField(t *testing.T) {
	tree := NewFieldTree()

	node := NewFieldNode("name")
	tree.AddField(node)

	if _, exists := tree.Fields["name"]; !exists {
		t.Error("Field 'name' should exist in tree")
	}
}

func TestFieldTree_AddFieldWithAlias(t *testing.T) {
	tree := NewFieldTree()

	node := NewFieldNode("name")
	node.Alias = "userName"
	tree.AddField(node)

	// Should be stored under alias
	if _, exists := tree.Fields["userName"]; !exists {
		t.Error("Field should be stored under alias 'userName'")
	}
	if _, exists := tree.Fields["name"]; exists {
		t.Error("Field should not be stored under original name when aliased")
	}
}

func TestRequestedFields_Has(t *testing.T) {
	// Build tree: { id, name, orders { total, items { price } } }
	tree := NewFieldTree()
	tree.AddField(NewFieldNode("id"))
	tree.AddField(NewFieldNode("name"))

	ordersNode := NewFieldNode("orders")
	ordersNode.IsLeaf = false
	ordersNode.Children = NewFieldTree()
	ordersNode.Children.AddField(NewFieldNode("total"))

	itemsNode := NewFieldNode("items")
	itemsNode.IsLeaf = false
	itemsNode.Children = NewFieldTree()
	itemsNode.Children.AddField(NewFieldNode("price"))
	ordersNode.Children.AddField(itemsNode)

	tree.AddField(ordersNode)

	rf := NewRequestedFields(tree)

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
		{"orders.items.quantity", false},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := rf.Has(tt.path); got != tt.expected {
				t.Errorf("Has(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestRequestedFields_Get(t *testing.T) {
	tree := NewFieldTree()

	idNode := NewFieldNode("id")
	tree.AddField(idNode)

	ordersNode := NewFieldNode("orders")
	ordersNode.IsLeaf = false
	ordersNode.Children = NewFieldTree()
	ordersNode.Children.AddField(NewFieldNode("total"))
	tree.AddField(ordersNode)

	rf := NewRequestedFields(tree)

	// Test getting existing node
	node := rf.Get("id")
	if node == nil {
		t.Fatal("Expected to get 'id' node")
	}
	if node.Name != "id" {
		t.Errorf("Expected name 'id', got %q", node.Name)
	}

	// Test getting nested node
	node = rf.Get("orders.total")
	if node == nil {
		t.Fatal("Expected to get 'orders.total' node")
	}
	if node.Name != "total" {
		t.Errorf("Expected name 'total', got %q", node.Name)
	}

	// Test getting non-existent node
	node = rf.Get("nonexistent")
	if node != nil {
		t.Error("Expected nil for non-existent path")
	}
}

func TestRequestedFields_List(t *testing.T) {
	tree := NewFieldTree()
	tree.AddField(NewFieldNode("id"))
	tree.AddField(NewFieldNode("name"))
	tree.AddField(NewFieldNode("email"))

	rf := NewRequestedFields(tree)
	fields := rf.List()

	sort.Strings(fields)
	expected := []string{"email", "id", "name"}

	if len(fields) != len(expected) {
		t.Fatalf("Expected %d fields, got %d", len(expected), len(fields))
	}

	for i, f := range expected {
		if fields[i] != f {
			t.Errorf("Expected field %q at index %d, got %q", f, i, fields[i])
		}
	}
}

func TestRequestedFields_ListFlat(t *testing.T) {
	// Build tree: { id, name, orders { id, total } }
	tree := NewFieldTree()
	tree.AddField(NewFieldNode("id"))
	tree.AddField(NewFieldNode("name"))

	ordersNode := NewFieldNode("orders")
	ordersNode.IsLeaf = false
	ordersNode.Children = NewFieldTree()
	ordersNode.Children.AddField(NewFieldNode("id"))
	ordersNode.Children.AddField(NewFieldNode("total"))
	tree.AddField(ordersNode)

	rf := NewRequestedFields(tree)
	fields := rf.ListFlat()

	sort.Strings(fields)
	expected := []string{"id", "name", "orders", "orders.id", "orders.total"}

	if len(fields) != len(expected) {
		t.Fatalf("Expected %d fields, got %d: %v", len(expected), len(fields), fields)
	}

	for i, f := range expected {
		if fields[i] != f {
			t.Errorf("Expected field %q at index %d, got %q", f, i, fields[i])
		}
	}
}

func TestRequestedFields_SubFields(t *testing.T) {
	// Build tree: { user { id, name, address { city, country } } }
	tree := NewFieldTree()

	userNode := NewFieldNode("user")
	userNode.IsLeaf = false
	userNode.Children = NewFieldTree()
	userNode.Children.AddField(NewFieldNode("id"))
	userNode.Children.AddField(NewFieldNode("name"))

	addressNode := NewFieldNode("address")
	addressNode.IsLeaf = false
	addressNode.Children = NewFieldTree()
	addressNode.Children.AddField(NewFieldNode("city"))
	addressNode.Children.AddField(NewFieldNode("country"))
	userNode.Children.AddField(addressNode)

	tree.AddField(userNode)

	rf := NewRequestedFields(tree)

	// Get subfields of user
	userFields := rf.SubFields("user")
	if !userFields.Has("id") {
		t.Error("Expected user subfields to have 'id'")
	}
	if !userFields.Has("name") {
		t.Error("Expected user subfields to have 'name'")
	}
	if !userFields.Has("address") {
		t.Error("Expected user subfields to have 'address'")
	}

	// Get subfields of user.address
	addressFields := rf.SubFields("user.address")
	if !addressFields.Has("city") {
		t.Error("Expected address subfields to have 'city'")
	}
	if !addressFields.Has("country") {
		t.Error("Expected address subfields to have 'country'")
	}
}

func TestRequestedFields_IsEmpty(t *testing.T) {
	// Empty tree
	rf := NewRequestedFields(NewFieldTree())
	if !rf.IsEmpty() {
		t.Error("Expected empty RequestedFields to be empty")
	}

	// Nil tree
	rf = NewRequestedFields(nil)
	if !rf.IsEmpty() {
		t.Error("Expected nil RequestedFields to be empty")
	}

	// Non-empty tree
	tree := NewFieldTree()
	tree.AddField(NewFieldNode("id"))
	rf = NewRequestedFields(tree)
	if rf.IsEmpty() {
		t.Error("Expected non-empty RequestedFields to not be empty")
	}
}

func TestRequestedFields_ListAtDepth(t *testing.T) {
	// Build tree: { id, name, orders { total, items { price } } }
	tree := NewFieldTree()
	tree.AddField(NewFieldNode("id"))
	tree.AddField(NewFieldNode("name"))

	ordersNode := NewFieldNode("orders")
	ordersNode.IsLeaf = false
	ordersNode.Children = NewFieldTree()
	ordersNode.Children.AddField(NewFieldNode("total"))

	itemsNode := NewFieldNode("items")
	itemsNode.IsLeaf = false
	itemsNode.Children = NewFieldTree()
	itemsNode.Children.AddField(NewFieldNode("price"))
	ordersNode.Children.AddField(itemsNode)

	tree.AddField(ordersNode)

	rf := NewRequestedFields(tree)

	// Depth 0: id, name, orders
	depth0 := rf.ListAtDepth(0)
	sort.Strings(depth0)
	if len(depth0) != 3 {
		t.Errorf("Expected 3 fields at depth 0, got %d: %v", len(depth0), depth0)
	}

	// Depth 1: orders.total, orders.items
	depth1 := rf.ListAtDepth(1)
	sort.Strings(depth1)
	if len(depth1) != 2 {
		t.Errorf("Expected 2 fields at depth 1, got %d: %v", len(depth1), depth1)
	}

	// Depth 2: orders.items.price
	depth2 := rf.ListAtDepth(2)
	if len(depth2) != 1 {
		t.Errorf("Expected 1 field at depth 2, got %d: %v", len(depth2), depth2)
	}
}

func TestFieldNode_Arguments(t *testing.T) {
	node := NewFieldNode("users")
	node.Arguments["limit"] = "10"
	node.Arguments["offset"] = "0"

	if node.Arguments["limit"] != "10" {
		t.Errorf("Expected limit '10', got %v", node.Arguments["limit"])
	}
	if node.Arguments["offset"] != "0" {
		t.Errorf("Expected offset '0', got %v", node.Arguments["offset"])
	}
}

func TestCalculateMaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		buildFn  func() *FieldTree
		expected int
	}{
		{
			name: "flat",
			buildFn: func() *FieldTree {
				tree := NewFieldTree()
				tree.AddField(NewFieldNode("id"))
				tree.AddField(NewFieldNode("name"))
				return tree
			},
			expected: 1,
		},
		{
			name: "one level nested",
			buildFn: func() *FieldTree {
				tree := NewFieldTree()
				tree.AddField(NewFieldNode("id"))

				nested := NewFieldNode("orders")
				nested.Children = NewFieldTree()
				nested.Children.AddField(NewFieldNode("total"))
				tree.AddField(nested)
				return tree
			},
			expected: 2,
		},
		{
			name: "two levels nested",
			buildFn: func() *FieldTree {
				tree := NewFieldTree()

				level1 := NewFieldNode("user")
				level1.Children = NewFieldTree()

				level2 := NewFieldNode("orders")
				level2.Children = NewFieldTree()
				level2.Children.AddField(NewFieldNode("total"))

				level1.Children.AddField(level2)
				tree.AddField(level1)
				return tree
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := tt.buildFn()
			depth := calculateMaxDepth(tree, 0)
			if depth != tt.expected {
				t.Errorf("Expected depth %d, got %d", tt.expected, depth)
			}
		})
	}
}

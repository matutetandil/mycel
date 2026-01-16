// Package analyzer provides GraphQL field extraction and analysis.
// It parses GraphQL AST to determine which fields the client requested,
// enabling query optimization and field pruning.
package analyzer

import (
	"strings"
)

// FieldTree represents the hierarchical structure of requested fields.
type FieldTree struct {
	Fields   map[string]*FieldNode
	Typename string // For union/interface types
}

// FieldNode represents a single requested field with its metadata.
type FieldNode struct {
	Name      string
	Alias     string                 // If aliased: { userName: name }
	Arguments map[string]interface{} // Arguments passed to the field
	Children  *FieldTree             // For nested objects
	IsLeaf    bool                   // True if no children
}

// NewFieldTree creates a new empty FieldTree.
func NewFieldTree() *FieldTree {
	return &FieldTree{
		Fields: make(map[string]*FieldNode),
	}
}

// NewFieldNode creates a new field node.
func NewFieldNode(name string) *FieldNode {
	return &FieldNode{
		Name:      name,
		Arguments: make(map[string]interface{}),
		IsLeaf:    true,
	}
}

// AddField adds a field to the tree.
func (ft *FieldTree) AddField(node *FieldNode) {
	key := node.Name
	if node.Alias != "" {
		key = node.Alias
	}
	ft.Fields[key] = node
}

// RequestedFields provides query helpers for working with field selection.
type RequestedFields struct {
	tree *FieldTree
}

// NewRequestedFields creates a new RequestedFields from a FieldTree.
func NewRequestedFields(tree *FieldTree) *RequestedFields {
	return &RequestedFields{tree: tree}
}

// Has checks if a field path is requested.
// Path can be nested: "orders.items.price"
func (rf *RequestedFields) Has(path string) bool {
	if rf.tree == nil {
		return false
	}

	parts := strings.Split(path, ".")
	current := rf.tree

	for _, part := range parts {
		if current == nil || current.Fields == nil {
			return false
		}

		node, exists := current.Fields[part]
		if !exists {
			return false
		}

		current = node.Children
	}

	return true
}

// Get retrieves a field node by path.
func (rf *RequestedFields) Get(path string) *FieldNode {
	if rf.tree == nil {
		return nil
	}

	parts := strings.Split(path, ".")
	current := rf.tree

	for i, part := range parts {
		if current == nil || current.Fields == nil {
			return nil
		}

		node, exists := current.Fields[part]
		if !exists {
			return nil
		}

		// If this is the last part, return the node
		if i == len(parts)-1 {
			return node
		}

		current = node.Children
	}

	return nil
}

// List returns all top-level field names.
func (rf *RequestedFields) List() []string {
	if rf.tree == nil || rf.tree.Fields == nil {
		return []string{}
	}

	fields := make([]string, 0, len(rf.tree.Fields))
	for name := range rf.tree.Fields {
		fields = append(fields, name)
	}
	return fields
}

// ListFlat returns all field paths flattened.
// Example: ["id", "name", "orders.id", "orders.total"]
func (rf *RequestedFields) ListFlat() []string {
	if rf.tree == nil {
		return []string{}
	}
	return flattenTree(rf.tree, "")
}

// ListAtDepth returns fields at a specific nesting depth.
// Depth 0 = top level, depth 1 = first nested level, etc.
func (rf *RequestedFields) ListAtDepth(depth int) []string {
	if rf.tree == nil {
		return []string{}
	}
	return fieldsAtDepth(rf.tree, "", depth, 0)
}

// Tree returns the underlying FieldTree.
func (rf *RequestedFields) Tree() *FieldTree {
	return rf.tree
}

// SubFields returns RequestedFields for a nested path.
func (rf *RequestedFields) SubFields(path string) *RequestedFields {
	node := rf.Get(path)
	if node == nil || node.Children == nil {
		return &RequestedFields{tree: NewFieldTree()}
	}
	return &RequestedFields{tree: node.Children}
}

// IsEmpty returns true if no fields are requested.
func (rf *RequestedFields) IsEmpty() bool {
	return rf.tree == nil || len(rf.tree.Fields) == 0
}

// flattenTree recursively flattens the field tree to paths.
func flattenTree(tree *FieldTree, prefix string) []string {
	if tree == nil || tree.Fields == nil {
		return []string{}
	}

	var result []string
	for name, node := range tree.Fields {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		result = append(result, path)

		// Recursively flatten children
		if node.Children != nil && len(node.Children.Fields) > 0 {
			childPaths := flattenTree(node.Children, path)
			result = append(result, childPaths...)
		}
	}

	return result
}

// fieldsAtDepth returns fields at a specific depth level.
func fieldsAtDepth(tree *FieldTree, prefix string, targetDepth, currentDepth int) []string {
	if tree == nil || tree.Fields == nil {
		return []string{}
	}

	var result []string
	for name, node := range tree.Fields {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		if currentDepth == targetDepth {
			result = append(result, path)
		} else if node.Children != nil {
			childFields := fieldsAtDepth(node.Children, path, targetDepth, currentDepth+1)
			result = append(result, childFields...)
		}
	}

	return result
}

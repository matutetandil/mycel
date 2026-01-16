// Package pruner provides result pruning functionality for GraphQL responses.
// It ensures that only requested fields are returned to the client,
// acting as a safety net even if upstream optimizations fail.
package pruner

import (
	"github.com/matutetandil/mycel/internal/graphql/analyzer"
)

// Prune removes fields not in the requested set from the data.
// This is the main entry point for result pruning.
func Prune(data interface{}, requested *analyzer.RequestedFields) interface{} {
	if requested == nil || requested.IsEmpty() {
		return data
	}
	return pruneValue(data, requested)
}

// pruneValue recursively prunes a value based on requested fields.
func pruneValue(data interface{}, requested *analyzer.RequestedFields) interface{} {
	if data == nil {
		return nil
	}

	switch v := data.(type) {
	case map[string]interface{}:
		return pruneMap(v, requested)
	case []interface{}:
		return pruneSlice(v, requested)
	case []map[string]interface{}:
		return pruneMapSlice(v, requested)
	default:
		return data
	}
}

// pruneMap removes unrequested fields from a map.
func pruneMap(data map[string]interface{}, requested *analyzer.RequestedFields) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range data {
		// Check if this field was requested
		if !requested.Has(key) {
			continue
		}

		// Get the field node to check for children
		node := requested.Get(key)
		if node == nil {
			continue
		}

		// If the field has children (nested object), prune recursively
		if node.Children != nil && len(node.Children.Fields) > 0 {
			subFields := analyzer.NewRequestedFields(node.Children)
			result[key] = pruneValue(value, subFields)
		} else {
			result[key] = value
		}
	}

	return result
}

// pruneSlice prunes each element of a slice.
func pruneSlice(data []interface{}, requested *analyzer.RequestedFields) []interface{} {
	result := make([]interface{}, len(data))
	for i, item := range data {
		result[i] = pruneValue(item, requested)
	}
	return result
}

// pruneMapSlice prunes each map in a slice of maps.
func pruneMapSlice(data []map[string]interface{}, requested *analyzer.RequestedFields) []map[string]interface{} {
	result := make([]map[string]interface{}, len(data))
	for i, item := range data {
		result[i] = pruneMap(item, requested)
	}
	return result
}

// PruneWithPaths prunes data using a flat list of field paths.
// This is useful when you have paths like ["id", "name", "orders.total"]
// instead of a RequestedFields structure.
func PruneWithPaths(data interface{}, paths []string) interface{} {
	tree := buildTreeFromPaths(paths)
	requested := analyzer.NewRequestedFields(tree)
	return Prune(data, requested)
}

// buildTreeFromPaths builds a FieldTree from a list of dot-separated paths.
func buildTreeFromPaths(paths []string) *analyzer.FieldTree {
	tree := analyzer.NewFieldTree()

	for _, path := range paths {
		addPathToTree(tree, path)
	}

	return tree
}

// addPathToTree adds a single path to the tree.
func addPathToTree(tree *analyzer.FieldTree, path string) {
	parts := splitPath(path)
	current := tree

	for i, part := range parts {
		// Check if node already exists
		existing, exists := current.Fields[part]
		if exists {
			// Navigate to existing node's children
			if existing.Children == nil && i < len(parts)-1 {
				// Need to add children
				existing.Children = analyzer.NewFieldTree()
				existing.IsLeaf = false
			}
			if existing.Children != nil {
				current = existing.Children
			}
		} else {
			// Create new node
			node := analyzer.NewFieldNode(part)
			if i < len(parts)-1 {
				// Not the last part, create children tree
				node.Children = analyzer.NewFieldTree()
				node.IsLeaf = false
			}
			current.AddField(node)
			if node.Children != nil {
				current = node.Children
			}
		}
	}
}

// splitPath splits a dot-separated path into parts.
func splitPath(path string) []string {
	var parts []string
	var current []byte

	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			if len(current) > 0 {
				parts = append(parts, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, path[i])
		}
	}

	if len(current) > 0 {
		parts = append(parts, string(current))
	}

	return parts
}

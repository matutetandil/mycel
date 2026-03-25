package ide

import (
	"fmt"
	"strings"
)

// RenameFieldResult contains all edits needed to rename a transform field within a flow.
type RenameFieldResult struct {
	// FlowName is the flow containing the field.
	FlowName string `json:"flowName"`

	// OldName is the current field name.
	OldName string `json:"oldName"`

	// NewName is the new field name.
	NewName string `json:"newName"`

	// Edits is the list of text edits to apply.
	Edits []TextEdit `json:"edits"`

	// AffectedLocations describes what was found (for confirmation dialog).
	AffectedLocations []string `json:"affectedLocations"`
}

// RenameField renames a transform field within a flow and updates all references.
// This covers:
//   - The transform mapping attribute name (email = "..." → user_email = "...")
//   - Named params in to.query SQL (:email → :user_email)
//   - Column names in to.query SQL (email → user_email in column lists)
//   - References in response block (output.email → output.user_email)
//   - References in other transform rules that use the field
func (e *Engine) RenameField(flowName, oldFieldName, newFieldName string) *RenameFieldResult {
	if oldFieldName == "" || newFieldName == "" || oldFieldName == newFieldName {
		return nil
	}

	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	flowEntity := e.index.Flows[flowName]
	if flowEntity == nil {
		return nil
	}

	fi, ok := e.index.Files[flowEntity.File]
	if !ok {
		return nil
	}

	var flowBlock *Block
	for _, b := range fi.Blocks {
		if b.Type == "flow" && b.Name == flowName {
			flowBlock = b
			break
		}
	}
	if flowBlock == nil {
		return nil
	}

	result := &RenameFieldResult{
		FlowName: flowName,
		OldName:  oldFieldName,
		NewName:  newFieldName,
	}

	file := flowEntity.File

	for _, child := range flowBlock.Children {
		switch child.Type {
		case "transform":
			// Rename the attribute itself
			for _, attr := range child.Attrs {
				if attr.Name == oldFieldName {
					// Replace just the attribute name, keep the value
					result.Edits = append(result.Edits, TextEdit{
						File: file,
						Range: Range{
							Start: Position{Line: attr.Range.Start.Line, Col: attr.Range.Start.Col},
							End:   Position{Line: attr.Range.Start.Line, Col: attr.Range.Start.Col + len(oldFieldName)},
						},
						NewText: newFieldName,
					})
					result.AffectedLocations = append(result.AffectedLocations,
						fmt.Sprintf("transform: %s = ...", oldFieldName))
				}

				// Check if other transform rules reference this field in their CEL expression
				if attr.Name != oldFieldName && attr.ValueRaw != "" {
					if containsFieldRef(attr.ValueRaw, oldFieldName) {
						newValue := replaceFieldRef(attr.ValueRaw, oldFieldName, newFieldName)
						result.Edits = append(result.Edits, TextEdit{
							File:    file,
							Range:   attr.ValRange,
							NewText: fmt.Sprintf("%q", newValue),
						})
						result.AffectedLocations = append(result.AffectedLocations,
							fmt.Sprintf("transform: %s references %s", attr.Name, oldFieldName))
					}
				}
			}

		case "to":
			// Check query for named params (:fieldName) and column names
			for _, attr := range child.Attrs {
				if attr.Name == "query" && attr.ValueRaw != "" {
					if strings.Contains(attr.ValueRaw, ":"+oldFieldName) ||
						strings.Contains(attr.ValueRaw, oldFieldName) {
						newQuery := replaceInSQL(attr.ValueRaw, oldFieldName, newFieldName)
						if newQuery != attr.ValueRaw {
							result.Edits = append(result.Edits, TextEdit{
								File:    file,
								Range:   attr.ValRange,
								NewText: fmt.Sprintf("%q", newQuery),
							})
							result.AffectedLocations = append(result.AffectedLocations,
								fmt.Sprintf("to.query: references :%s", oldFieldName))
						}
					}
				}

				// Check target — if using implicit column mapping, the field name IS the column
				if attr.Name == "target" {
					// Implicit mapping doesn't need text changes — the column name
					// comes from the transform output field name, which we're already renaming
				}
			}

		case "response":
			// Check for output.fieldName references
			for _, attr := range child.Attrs {
				if attr.ValueRaw != "" && containsFieldRef(attr.ValueRaw, "output."+oldFieldName) {
					newValue := replaceFieldRef(attr.ValueRaw, "output."+oldFieldName, "output."+newFieldName)
					result.Edits = append(result.Edits, TextEdit{
						File:    file,
						Range:   attr.ValRange,
						NewText: fmt.Sprintf("%q", newValue),
					})
					result.AffectedLocations = append(result.AffectedLocations,
						fmt.Sprintf("response: %s references output.%s", attr.Name, oldFieldName))
				}
			}
		}
	}

	if len(result.Edits) == 0 {
		return nil
	}

	return result
}

// replaceInSQL replaces field references in SQL queries.
// Handles both named params (:fieldName) and column names.
func replaceInSQL(sql, oldName, newName string) string {
	// Replace named params: :oldName → :newName
	result := strings.ReplaceAll(sql, ":"+oldName, ":"+newName)

	// Replace column names in column lists (careful not to replace inside strings)
	// Simple approach: replace word-boundary matches
	// Handle common SQL patterns: (col1, col2) and col1 = :col1
	result = replaceSQLIdentifier(result, oldName, newName)

	return result
}

// replaceSQLIdentifier replaces a SQL identifier respecting word boundaries.
// Replaces "email" but not "email_address" or "user_email".
func replaceSQLIdentifier(sql, oldName, newName string) string {
	var result strings.Builder
	i := 0
	for i < len(sql) {
		// Check if we found the old name at this position
		if i+len(oldName) <= len(sql) && sql[i:i+len(oldName)] == oldName {
			// Check word boundaries
			before := i == 0 || !isIdentChar(sql[i-1])
			after := i+len(oldName) >= len(sql) || !isIdentChar(sql[i+len(oldName)])

			// Don't replace if it's already a named param (handled above)
			isParam := i > 0 && sql[i-1] == ':'

			if before && after && !isParam {
				result.WriteString(newName)
				i += len(oldName)
				continue
			}
		}
		result.WriteByte(sql[i])
		i++
	}
	return result.String()
}

// isIdentChar returns true if the character is valid in a SQL/HCL identifier.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// containsFieldRef checks if a CEL expression references a field.
func containsFieldRef(expr, fieldName string) bool {
	return strings.Contains(expr, fieldName)
}

// replaceFieldRef replaces field references in a CEL expression.
func replaceFieldRef(expr, oldRef, newRef string) string {
	return strings.ReplaceAll(expr, oldRef, newRef)
}

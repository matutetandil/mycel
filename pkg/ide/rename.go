package ide

// RenameEdit represents a text edit for a rename operation.
type RenameEdit struct {
	File     string `json:"file"`
	Range    Range  `json:"range"`
	NewText  string `json:"newText"`
}

// Rename renames an entity and returns all edits needed across the project.
// The cursor must be on a block label or a reference value.
func (e *Engine) Rename(path string, line, col int, newName string) []RenameEdit {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok || newName == "" {
		return nil
	}

	// Find what we're renaming
	ctx := findCursorContext(fi, line, col)

	var kind, oldName string

	// Case 1: cursor on a reference value (e.g., connector = "api")
	if ctx.InValue && ctx.AttrName != "" && ctx.Block != nil {
		schema := lookupBlockSchema(ctx.BlockPath)
		if schema != nil {
			as := findAttrSchema(schema.Attrs, ctx.AttrName)
			if as != nil && as.Ref != RefNone {
				for _, attr := range ctx.Block.Attrs {
					if attr.Name == ctx.AttrName {
						oldName = attr.ValueRaw
						break
					}
				}
				switch as.Ref {
				case RefConnector:
					kind = "connector"
				case RefFlow:
					kind = "flow"
				case RefType:
					kind = "type"
				case RefTransform:
					kind = "transform"
				}
			}
		}
	}

	// Case 2: cursor is on a block (not in a value) — rename the definition
	if kind == "" && ctx.Block != nil && ctx.Block.Name != "" && !ctx.InValue {
		kind = ctx.Block.Type
		oldName = ctx.Block.Name
	}

	if kind == "" || oldName == "" {
		return nil
	}

	return e.computeRenameEdits(kind, oldName, newName)
}

// computeRenameEdits finds all locations where an entity is defined or referenced.
func (e *Engine) computeRenameEdits(kind, oldName, newName string) []RenameEdit {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	var edits []RenameEdit

	for _, fi := range e.index.Files {
		for _, b := range fi.Blocks {
			edits = append(edits, findRenameEditsInBlock(fi.Path, b, kind, oldName, newName)...)
		}
	}

	return edits
}

// findRenameEditsInBlock recursively finds rename targets in a block.
func findRenameEditsInBlock(path string, b *Block, kind, oldName, newName string) []RenameEdit {
	var edits []RenameEdit

	// Check if this block IS the definition being renamed
	if b.Type == kind && b.Name == oldName {
		edits = append(edits, RenameEdit{
			File:    path,
			Range:   b.Range, // The block label position
			NewText: newName,
		})
	}

	// Check attributes for references
	schema := findBlockSchemaByType(b.Type)
	if schema != nil {
		for _, attr := range b.Attrs {
			as := findAttrSchema(schema.Attrs, attr.Name)
			if as == nil || as.Ref == RefNone || attr.ValueRaw != oldName {
				continue
			}

			refKind := ""
			switch as.Ref {
			case RefConnector:
				refKind = "connector"
			case RefFlow:
				refKind = "flow"
			case RefType:
				refKind = "type"
			case RefTransform:
				refKind = "transform"
			}

			if refKind == kind {
				edits = append(edits, RenameEdit{
					File:    path,
					Range:   attr.ValRange,
					NewText: newName,
				})
			}
		}
	}

	for _, child := range b.Children {
		edits = append(edits, findRenameEditsInBlock(path, child, kind, oldName, newName)...)
	}

	return edits
}

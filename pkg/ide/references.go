package ide

// Reference represents a location where an entity is referenced.
type Reference struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	AttrName  string `json:"attrName"`  // the attribute name (e.g., "connector")
	BlockType string `json:"blockType"` // the containing block type (e.g., "from")
	BlockName string `json:"blockName"` // the containing block name (e.g., "get_users")
}

// FindReferences returns all locations where the given entity is referenced.
// kind is "connector", "flow", "type", "transform", etc.
// name is the entity name (e.g., "api", "get_users").
// Returns the definition location + all reference locations.
func (e *Engine) FindReferences(kind, name string) []Reference {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	var refs []Reference

	// Find the definition
	entity := e.index.lookupEntityUnlocked(kind, name)
	if entity != nil {
		refs = append(refs, Reference{
			File:      entity.File,
			Line:      entity.Range.Start.Line,
			BlockType: kind,
			BlockName: name,
		})
	}

	// Find all references across all files
	for _, fi := range e.index.Files {
		for _, b := range fi.Blocks {
			refs = append(refs, findRefsInBlock(fi.Path, b, kind, name)...)
		}
	}

	return refs
}

// findRefsInBlock recursively finds references to an entity within a block.
func findRefsInBlock(path string, b *Block, kind, name string) []Reference {
	var refs []Reference

	// Check attributes for references
	schema := findBlockSchemaByType(b.Type)
	if schema != nil {
		for _, attr := range b.Attrs {
			as := findAttrSchema(schema.Attrs, attr.Name)
			if as == nil || as.Ref == RefNone || attr.ValueRaw != name {
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
			case RefCache:
				refKind = "cache"
			case RefValidator:
				refKind = "validator"
			}

			if refKind == kind {
				refs = append(refs, Reference{
					File:      path,
					Line:      attr.Range.Start.Line,
					Col:       attr.ValRange.Start.Col,
					AttrName:  attr.Name,
					BlockType: b.Type,
					BlockName: b.Name,
				})
			}
		}
	}

	for _, child := range b.Children {
		refs = append(refs, findRefsInBlock(path, child, kind, name)...)
	}

	return refs
}

// RenameEntity renames an entity and returns all edits needed across the project.
// Unlike Rename() which works by cursor position, this works by entity kind and name directly.
// Returns edits for the definition (block label) + all references (attribute values).
func (e *Engine) RenameEntity(kind, oldName, newName string) []RenameEdit {
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}
	return e.computeRenameEdits(kind, oldName, newName)
}

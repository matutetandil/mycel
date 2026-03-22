package ide

import "fmt"

// diagnoseFile returns diagnostics for a single file (parse errors + schema validation).
func diagnoseFile(fi *FileIndex) []*Diagnostic {
	var diags []*Diagnostic

	// Layer 1: HCL parse errors
	diags = append(diags, fi.ParseDiags...)

	// Layer 2: Schema validation
	diags = append(diags, validateBlocks(fi.Path, fi.Blocks, rootSchema())...)

	// Layer 2.5: Connector-type-specific validation + operation validation
	for _, b := range fi.Blocks {
		if b.Type == "connector" {
			diags = append(diags, validateConnectorType(fi.Path, b)...)
		}
		if b.Type == "flow" {
			diags = append(diags, validateFlowOperations(fi.Path, b)...)
		}
	}

	return diags
}

// validateFlowOperations validates operation strings in from/to blocks.
func validateFlowOperations(path string, flowBlock *Block) []*Diagnostic {
	var diags []*Diagnostic
	for _, child := range flowBlock.Children {
		if child.Type == "from" || child.Type == "to" {
			for _, attr := range child.Attrs {
				if attr.Name == "operation" {
					connType := resolveConnectorType(child)
					diags = append(diags, validateOperation(path, attr, connType)...)
				}
			}
		}
	}
	return diags
}

// resolveConnectorType returns the connector type for a from/to block if available.
func resolveConnectorType(b *Block) string {
	// Can't resolve without the index here — return empty to skip type-specific validation.
	// The cross-ref phase handles this.
	return ""
}

// diagnoseCrossRefs returns cross-reference diagnostics across the project.
func diagnoseCrossRefs(idx *ProjectIndex) []*Diagnostic {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var diags []*Diagnostic

	for _, fi := range idx.Files {
		diags = append(diags, validateRefs(fi, idx)...)
		diags = append(diags, validateDuplicates(fi, idx)...)
	}

	return diags
}

// validateBlocks checks blocks against their schema.
func validateBlocks(path string, blocks []*Block, schemas []BlockSchema) []*Diagnostic {
	var diags []*Diagnostic

	validTypes := make(map[string]bool)
	for _, s := range schemas {
		validTypes[s.Type] = true
	}

	for _, b := range blocks {
		if !validTypes[b.Type] {
			diags = append(diags, &Diagnostic{
				Severity: SeverityError,
				Message:  fmt.Sprintf("unknown block type %q", b.Type),
				File:     path,
				Range:    b.Range,
			})
			continue
		}

		// Find matching schema
		var schema *BlockSchema
		for i := range schemas {
			if schemas[i].Type == b.Type {
				schema = &schemas[i]
				break
			}
		}

		if schema == nil {
			continue
		}

		// Check required attributes
		for _, as := range schema.Attrs {
			if as.Required && !b.HasAttr(as.Name) {
				// Only flag missing required attrs if the block has a body (not empty)
				if len(b.Attrs) > 0 || len(b.Children) > 0 {
					diags = append(diags, &Diagnostic{
						Severity: SeverityError,
						Message:  fmt.Sprintf("missing required attribute %q in %s block", as.Name, b.Type),
						File:     path,
						Range:    b.Range,
					})
				}
			}
		}

		// Check attribute values against enums
		for _, attr := range b.Attrs {
			as := findAttrSchema(schema.Attrs, attr.Name)
			if as != nil && len(as.Values) > 0 && attr.ValueRaw != "" {
				if !contains(as.Values, attr.ValueRaw) {
					diags = append(diags, &Diagnostic{
						Severity: SeverityError,
						Message:  fmt.Sprintf("invalid value %q for %s.%s (valid: %v)", attr.ValueRaw, b.Type, attr.Name, as.Values),
						File:     path,
						Range:    attr.ValRange,
					})
				}
			}
		}

		// Validate children recursively
		if len(b.Children) > 0 && len(schema.Children) > 0 {
			diags = append(diags, validateBlocks(path, b.Children, schema.Children)...)
		}
	}

	return diags
}

// validateRefs checks that connector, type, and transform references exist.
func validateRefs(fi *FileIndex, idx *ProjectIndex) []*Diagnostic {
	var diags []*Diagnostic

	for _, b := range fi.Blocks {
		diags = append(diags, validateBlockRefs(fi.Path, b, idx)...)
	}

	return diags
}

// validateBlockRefs recursively checks references within a block.
func validateBlockRefs(path string, b *Block, idx *ProjectIndex) []*Diagnostic {
	var diags []*Diagnostic

	// Find schema for this block to know which attrs are refs
	schema := findBlockSchemaByType(b.Type)

	if schema != nil {
		for _, attr := range b.Attrs {
			as := findAttrSchema(schema.Attrs, attr.Name)
			if as == nil || as.Ref == RefNone || attr.ValueRaw == "" {
				continue
			}

			switch as.Ref {
			case RefConnector:
				if idx.Connectors[attr.ValueRaw] == nil {
					diags = append(diags, &Diagnostic{
						Severity: SeverityError,
						Message:  fmt.Sprintf("undefined connector %q", attr.ValueRaw),
						File:     path,
						Range:    attr.ValRange,
					})
				}
			case RefType:
				typeName := attr.ValueRaw
				// Handle "type.name" format
				if len(typeName) > 5 && typeName[:5] == "type." {
					typeName = typeName[5:]
				}
				if idx.Types[typeName] == nil {
					diags = append(diags, &Diagnostic{
						Severity: SeverityWarning,
						Message:  fmt.Sprintf("undefined type %q", attr.ValueRaw),
						File:     path,
						Range:    attr.ValRange,
					})
				}
			case RefTransform:
				if idx.Transforms[attr.ValueRaw] == nil {
					diags = append(diags, &Diagnostic{
						Severity: SeverityWarning,
						Message:  fmt.Sprintf("undefined transform %q", attr.ValueRaw),
						File:     path,
						Range:    attr.ValRange,
					})
				}
			case RefFlow:
				if idx.Flows[attr.ValueRaw] == nil {
					diags = append(diags, &Diagnostic{
						Severity: SeverityError,
						Message:  fmt.Sprintf("undefined flow %q", attr.ValueRaw),
						File:     path,
						Range:    attr.ValRange,
					})
				}
			}
		}
	}

	for _, child := range b.Children {
		diags = append(diags, validateBlockRefs(path, child, idx)...)
	}

	return diags
}

// validateDuplicates checks for duplicate names within this file against the project.
func validateDuplicates(fi *FileIndex, idx *ProjectIndex) []*Diagnostic {
	var diags []*Diagnostic

	for _, b := range fi.Blocks {
		if b.Name == "" {
			continue
		}

		entity := idx.lookupEntityUnlocked(b.Type, b.Name)
		if entity != nil && entity.File != fi.Path {
			diags = append(diags, &Diagnostic{
				Severity: SeverityError,
				Message:  fmt.Sprintf("duplicate %s name %q (also defined in %s)", b.Type, b.Name, entity.File),
				File:     fi.Path,
				Range:    b.Range,
			})
		}
	}

	return diags
}

// lookupEntityUnlocked finds an entity without locking (caller must hold lock).
func (idx *ProjectIndex) lookupEntityUnlocked(kind, name string) *NamedEntity {
	switch kind {
	case "connector":
		return idx.Connectors[name]
	case "flow":
		return idx.Flows[name]
	case "type":
		return idx.Types[name]
	case "transform":
		return idx.Transforms[name]
	case "aspect":
		return idx.Aspects[name]
	case "validator":
		return idx.Validators[name]
	case "cache":
		return idx.Caches[name]
	case "saga":
		return idx.Sagas[name]
	case "state_machine":
		return idx.StateMachines[name]
	}
	return nil
}

// findBlockSchemaByType returns the schema for a block type, searching all levels.
func findBlockSchemaByType(blockType string) *BlockSchema {
	// Search root schemas
	for _, s := range rootSchema() {
		if s.Type == blockType {
			return &s
		}
		// Search children
		if found := findInChildren(s.Children, blockType); found != nil {
			return found
		}
	}
	return nil
}

func findInChildren(schemas []BlockSchema, blockType string) *BlockSchema {
	for _, s := range schemas {
		if s.Type == blockType {
			return &s
		}
		if found := findInChildren(s.Children, blockType); found != nil {
			return found
		}
	}
	return nil
}

// findAttrSchema finds an attribute schema by name.
func findAttrSchema(schemas []AttrSchema, name string) *AttrSchema {
	for i := range schemas {
		if schemas[i].Name == name {
			return &schemas[i]
		}
	}
	return nil
}

// contains returns true if the slice contains the value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

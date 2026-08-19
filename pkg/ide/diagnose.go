package ide

import (
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// diagnoseFile returns diagnostics for a single file (parse errors + schema validation).
func diagnoseFile(fi *FileIndex, reg *schema.Registry) []*Diagnostic {
	var diags []*Diagnostic

	// Layer 1: HCL parse errors
	diags = append(diags, fi.ParseDiags...)

	// Layer 2: Schema validation
	diags = append(diags, validateBlocks(fi.Path, fi.Blocks, rootSchema(), reg)...)

	// Layer 2.5: Connector-type-specific validation + operation validation
	for _, b := range fi.Blocks {
		if b.Type == "connector" {
			diags = append(diags, validateConnectorType(fi.Path, b, reg)...)
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
func validateBlocks(path string, blocks []*Block, schemas []BlockSchema, reg *schema.Registry) []*Diagnostic {
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

		// Build the full set of known attributes for this block
		// (base schema + connector-type-specific if applicable)
		knownAttrs := make(map[string]*AttrSchema)
		for i := range schema.Attrs {
			knownAttrs[schema.Attrs[i].Name] = &schema.Attrs[i]
		}
		if b.Type == "connector" {
			connType := b.GetAttr("type")
			driver := b.GetAttr("driver")
			for _, ta := range connectorTypeAttrsWithRegistry(reg, connType, driver) {
				ta := ta
				knownAttrs[ta.Name] = &ta
			}
		}

		// Check for unknown attributes
		// Skip open/dynamic blocks where any attribute name is valid
		isDynamic := b.Type == "transform" || b.Type == "response" || schema.Open
		if !isDynamic {
			for _, attr := range b.Attrs {
				if _, known := knownAttrs[attr.Name]; !known {
					diags = append(diags, &Diagnostic{
						Severity: SeverityError,
						Message:  fmt.Sprintf("unknown attribute %q in %s block", attr.Name, b.Type),
						File:     path,
						Range:    attr.Range,
					})
				}
			}
		}

		// Check attribute values against enums (including connector-type-specific)
		for _, attr := range b.Attrs {
			as := knownAttrs[attr.Name]
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

			// Check conditional required attributes
			if as != nil && as.Required && !b.HasAttr(as.Name) {
				diags = append(diags, &Diagnostic{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("%s connector requires attribute %q", b.GetAttr("type"), as.Name),
					File:     path,
					Range:    b.Range,
				})
			}
		}

		// Validate children recursively
		if len(b.Children) > 0 && len(schema.Children) > 0 {
			diags = append(diags, validateBlocks(path, b.Children, schema.Children, reg)...)
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

			if diag := checkReference(path, as.Ref, attr, idx); diag != nil {
				diags = append(diags, diag)
			}
		}
	}

	for _, child := range b.Children {
		diags = append(diags, validateBlockRefs(path, child, idx)...)
	}

	return diags
}

// referenceKinds says, for every kind of reference the schema can mark, which
// top-level block declares its target and how a missing one reads.
//
// A table rather than a switch, so that a kind added to the schema and not
// handled here fails a test instead of being passed over in silence — which is
// what happened to validators, to named caches and to all ten reusable kinds:
// `mycel validate` refuses `use = "lock.typo"` by name, and the editor said
// nothing at all, while an undefined connector one line above drew a squiggle.
//
// The block type is also the prefix a reference may carry: both
// `use = "lock.slow"` and `use = "slow"` resolve, because the parser strips a
// leading "<kind>." before looking the name up.
var referenceKinds = map[RefKind]struct {
	blockType string
	noun      string
	severity  Severity
}{
	RefConnector:     {"connector", "connector", SeverityError},
	RefFlow:          {"flow", "flow", SeverityError},
	RefType:          {"type", "type", SeverityWarning},
	RefTransform:     {"transform", "transform", SeverityWarning},
	RefValidator:     {"validator", "validator", SeverityWarning},
	RefCache:         {"cache", "cache", SeverityWarning},
	RefStateMachine:  {"state_machine", "state machine", SeverityWarning},
	RefDedupe:        {"dedupe", "dedupe block", SeverityWarning},
	RefRetry:         {"retry", "retry block", SeverityWarning},
	RefLock:          {"lock", "lock block", SeverityWarning},
	RefSemaphore:     {"semaphore", "semaphore block", SeverityWarning},
	RefSequenceGuard: {"sequence_guard", "sequence_guard block", SeverityWarning},
	RefCoordinate:    {"coordinate", "coordinate block", SeverityWarning},
	RefTransaction:   {"transaction", "transaction block", SeverityWarning},
	RefErrorHandling: {"error_handling", "error_handling block", SeverityWarning},
	RefAccept:        {"accept", "accept block", SeverityWarning},
	RefResponse:      {"response", "response block", SeverityWarning},
}

// checkReference reports a name whose target no block declares.
func checkReference(path string, kind RefKind, attr *Attribute, idx *ProjectIndex) *Diagnostic {
	rule, known := referenceKinds[kind]
	if !known {
		return nil
	}

	name := strings.TrimPrefix(attr.ValueRaw, rule.blockType+".")
	if idx.lookupEntity(rule.blockType, name) != nil {
		return nil
	}
	return &Diagnostic{
		Severity: rule.severity,
		Message:  fmt.Sprintf("undefined %s %q", rule.noun, attr.ValueRaw),
		File:     path,
		Range:    attr.ValRange,
	}
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

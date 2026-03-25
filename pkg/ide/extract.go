package ide

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ExtractTransformResult contains the edits needed to extract an inline
// transform from a flow into a named reusable transform.
type ExtractTransformResult struct {
	// Name is the generated transform name.
	Name string `json:"name"`

	// FlowEdit replaces the inline transform block with `transform { use = "name" }`.
	FlowEdit TextEdit `json:"flowEdit"`

	// NewTransform is the full text of the new named transform block.
	// Studio writes this to a new file or appends to an existing transforms file.
	NewTransform string `json:"newTransform"`

	// SuggestedFile is the recommended file path for the new transform.
	SuggestedFile string `json:"suggestedFile"`
}

// ExtractTransform extracts an inline transform from a flow into a named reusable transform.
// The flow's inline transform is replaced with `transform { use = "name" }`.
//
// flowName: the flow containing the inline transform.
// transformName: the desired name for the new named transform (if empty, derived from flow name).
func (e *Engine) ExtractTransform(flowName, transformName string) *ExtractTransformResult {
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

	// Find the flow block
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

	// Find the inline transform block
	var transformBlock *Block
	for _, child := range flowBlock.Children {
		if child.Type == "transform" {
			transformBlock = child
			break
		}
	}
	if transformBlock == nil {
		return nil
	}

	// Check if it already uses a named transform
	if transformBlock.GetAttr("use") != "" {
		return nil // already referencing a named transform
	}

	// Must have at least one mapping
	mappings := filterMappings(transformBlock.Attrs)
	if len(mappings) == 0 {
		return nil
	}

	// Generate transform name if not provided
	if transformName == "" {
		transformName = flowName + "_transform"
	}

	// Build the named transform block text
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("transform %q {\n", transformName))
	for _, attr := range mappings {
		if attr.ValueRaw != "" {
			sb.WriteString(fmt.Sprintf("  %s = %q\n", attr.Name, attr.ValueRaw))
		} else {
			// For dynamic expressions, we need to preserve the original text.
			// Since we don't have the raw source, use the attribute name as placeholder.
			sb.WriteString(fmt.Sprintf("  %s = \"\"\n", attr.Name))
		}
	}
	sb.WriteString("}\n")

	// Build the replacement for the inline transform
	replacement := fmt.Sprintf("transform {\n    use = %q\n  }", transformName)

	// Suggest file path
	flowDir := filepath.Dir(flowEntity.File)
	projectDir := filepath.Dir(flowDir)
	suggestedFile := filepath.Join(projectDir, "transforms", transformName+".mycel")

	return &ExtractTransformResult{
		Name: transformName,
		FlowEdit: TextEdit{
			File: flowEntity.File,
			Range: Range{
				Start: Position{Line: transformBlock.Range.Start.Line, Col: transformBlock.Range.Start.Col},
				End:   Position{Line: transformBlock.Range.End.Line, Col: transformBlock.Range.End.Col},
			},
			NewText: replacement,
		},
		NewTransform:  sb.String(),
		SuggestedFile: suggestedFile,
	}
}

// filterMappings returns attributes that are CEL mappings (excludes "use").
func filterMappings(attrs []*Attribute) []*Attribute {
	var result []*Attribute
	for _, a := range attrs {
		if a.Name != "use" {
			result = append(result, a)
		}
	}
	return result
}

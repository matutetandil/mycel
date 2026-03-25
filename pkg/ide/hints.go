package ide

import (
	"fmt"
	"path/filepath"
	"strings"
)

// HintKind classifies an organization hint.
type HintKind int

const (
	// HintMultipleBlocksInFile — file has more than one block of the same type.
	HintMultipleBlocksInFile HintKind = 1
	// HintFileNameMismatch — file name doesn't match the block name inside.
	HintFileNameMismatch HintKind = 2
	// HintMixedTypesInFile — file contains blocks of different types.
	HintMixedTypesInFile HintKind = 3
	// HintWrongDirectory — block type doesn't match the parent directory name.
	HintWrongDirectory HintKind = 4
)

// Hint represents an organization suggestion for the user.
type Hint struct {
	Kind    HintKind `json:"kind"`
	Message string   `json:"message"`
	File    string   `json:"file"`
	Range   Range    `json:"range"`

	// SuggestedFile is the recommended file path (for refactoring).
	// Empty if the hint doesn't involve moving/renaming.
	SuggestedFile string `json:"suggestedFile,omitempty"`

	// BlockType and BlockName identify the block this hint refers to.
	BlockType string `json:"blockType,omitempty"`
	BlockName string `json:"blockName,omitempty"`
}

// Hints returns organization suggestions for the entire project.
// These are informational — not errors. Studio shows them as light bulb indicators.
func (e *Engine) Hints() []Hint {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	var hints []Hint

	for _, fi := range e.index.Files {
		hints = append(hints, hintsForFile(fi)...)
	}

	return hints
}

// HintsForFile returns organization suggestions for a specific file.
func (e *Engine) HintsForFile(path string) []Hint {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	return hintsForFile(fi)
}

// hintsForFile analyzes a single file for organization issues.
func hintsForFile(fi *FileIndex) []Hint {
	if len(fi.Blocks) == 0 {
		return nil
	}

	var hints []Hint

	// Skip config.mycel — it's expected to have a single service block
	baseName := filepath.Base(fi.Path)
	if baseName == "config.mycel" {
		return nil
	}

	// Rule 1: Multiple blocks of the same type → suggest splitting
	hints = append(hints, checkMultipleBlocks(fi)...)

	// Rule 2: File name doesn't match block name → suggest renaming
	hints = append(hints, checkFileNameMismatch(fi)...)

	// Rule 3: Mixed block types in one file → suggest separating
	hints = append(hints, checkMixedTypes(fi)...)

	// Rule 4: Block in wrong directory → suggest moving
	hints = append(hints, checkWrongDirectory(fi)...)

	return hints
}

// checkMultipleBlocks flags files with more than one block of the same type.
// Example: connectors.mycel has 3 connectors → suggest splitting into api.mycel, db.mycel, rabbit.mycel
func checkMultipleBlocks(fi *FileIndex) []Hint {
	// Count blocks by type
	typeCounts := make(map[string][]*Block)
	for _, b := range fi.Blocks {
		if b.Name != "" {
			typeCounts[b.Type] = append(typeCounts[b.Type], b)
		}
	}

	var hints []Hint
	for blockType, blocks := range typeCounts {
		if len(blocks) <= 1 {
			continue
		}

		dir := filepath.Dir(fi.Path)

		for _, b := range blocks {
			suggested := filepath.Join(dir, toFileName(b.Name))
			hints = append(hints, Hint{
				Kind:          HintMultipleBlocksInFile,
				Message:       fmt.Sprintf("Consider moving %s %q to its own file", blockType, b.Name),
				File:          fi.Path,
				Range:         b.Range,
				SuggestedFile: suggested,
				BlockType:     blockType,
				BlockName:     b.Name,
			})
		}
	}

	return hints
}

// checkFileNameMismatch flags files where the name doesn't match the single block inside.
// Example: flow.mycel contains flow "save_customer" → suggest renaming to save_customer.mycel
func checkFileNameMismatch(fi *FileIndex) []Hint {
	// Only applies to files with exactly one named block
	if len(fi.Blocks) != 1 {
		return nil
	}

	b := fi.Blocks[0]
	if b.Name == "" {
		return nil
	}

	baseName := filepath.Base(fi.Path)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	expectedName := toBaseName(b.Name)

	// Allow plural forms as directory-style names (e.g., connectors.mycel for a single connector is fine)
	if nameWithoutExt == expectedName {
		return nil
	}

	// Don't flag generic names that are common conventions
	if isGenericFileName(nameWithoutExt, b.Type) {
		return nil
	}

	dir := filepath.Dir(fi.Path)
	suggested := filepath.Join(dir, toFileName(b.Name))

	return []Hint{{
		Kind:          HintFileNameMismatch,
		Message:       fmt.Sprintf("File %q contains %s %q — consider renaming to %s", baseName, b.Type, b.Name, filepath.Base(suggested)),
		File:          fi.Path,
		Range:         b.Range,
		SuggestedFile: suggested,
		BlockType:     b.Type,
		BlockName:     b.Name,
	}}
}

// checkMixedTypes flags files that contain blocks of different types.
// Example: a file with a connector AND a flow → suggest separating
func checkMixedTypes(fi *FileIndex) []Hint {
	types := make(map[string]bool)
	for _, b := range fi.Blocks {
		types[b.Type] = true
	}

	if len(types) <= 1 {
		return nil
	}

	typeList := make([]string, 0, len(types))
	for t := range types {
		typeList = append(typeList, t)
	}

	return []Hint{{
		Kind:    HintMixedTypesInFile,
		Message: fmt.Sprintf("File contains mixed block types (%s) — consider separating into different files", strings.Join(typeList, ", ")),
		File:    fi.Path,
		Range:   fi.Blocks[0].Range,
	}}
}

// checkWrongDirectory flags blocks whose type doesn't match the parent directory.
// Example: flows/database.mycel contains a connector → suggest moving to connectors/
func checkWrongDirectory(fi *FileIndex) []Hint {
	dir := filepath.Base(filepath.Dir(fi.Path))

	// Map directory names to expected block types
	dirToType := map[string]string{
		"connectors": "connector",
		"flows":      "flow",
		"types":      "type",
		"transforms": "transform",
		"aspects":    "aspect",
		"validators": "validator",
		"sagas":      "saga",
	}

	expectedType, hasMapping := dirToType[dir]
	if !hasMapping {
		return nil
	}

	var hints []Hint
	for _, b := range fi.Blocks {
		if b.Type != expectedType && b.Name != "" {
			// Suggest the correct directory
			correctDir := typeToDir(b.Type)
			if correctDir == "" {
				continue
			}

			projectDir := filepath.Dir(filepath.Dir(fi.Path))
			suggested := filepath.Join(projectDir, correctDir, toFileName(b.Name))

			hints = append(hints, Hint{
				Kind:          HintWrongDirectory,
				Message:       fmt.Sprintf("%s %q is in %s/ directory — consider moving to %s/", b.Type, b.Name, dir, correctDir),
				File:          fi.Path,
				Range:         b.Range,
				SuggestedFile: suggested,
				BlockType:     b.Type,
				BlockName:     b.Name,
			})
		}
	}

	return hints
}

// toFileName converts a block name to a file name.
// "save_customer" → "save_customer.mycel"
// "my-api" → "my-api.mycel"
func toFileName(name string) string {
	return toBaseName(name) + ".mycel"
}

// toBaseName converts a block name to a clean base name.
func toBaseName(name string) string {
	// Replace spaces and special chars with underscores
	name = strings.ReplaceAll(name, " ", "_")
	return strings.ToLower(name)
}

// isGenericFileName returns true if the filename is a common generic name for the block type.
// These are acceptable and shouldn't trigger a rename hint.
func isGenericFileName(name, blockType string) bool {
	// Singular and plural forms of the block type
	generics := map[string][]string{
		"connector": {"connector", "connectors", "conn", "database", "api", "mq", "rest"},
		"flow":      {"flow", "flows"},
		"type":      {"type", "types", "schema", "schemas"},
		"transform": {"transform", "transforms"},
		"aspect":    {"aspect", "aspects"},
		"validator": {"validator", "validators"},
		"saga":      {"saga", "sagas"},
		"cache":     {"cache", "caches"},
		"service":   {"service", "config"},
	}

	if allowed, ok := generics[blockType]; ok {
		for _, g := range allowed {
			if name == g {
				return true
			}
		}
	}

	return false
}

// typeToDir maps a block type to its conventional directory name.
func typeToDir(blockType string) string {
	switch blockType {
	case "connector":
		return "connectors"
	case "flow":
		return "flows"
	case "type":
		return "types"
	case "transform":
		return "transforms"
	case "aspect":
		return "aspects"
	case "validator":
		return "validators"
	case "saga":
		return "sagas"
	}
	return ""
}

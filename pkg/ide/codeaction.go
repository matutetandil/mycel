package ide

import "fmt"

// CodeActionKind classifies a code action.
type CodeActionKind int

const (
	CodeActionQuickFix CodeActionKind = 1
	CodeActionRefactor CodeActionKind = 2
)

// CodeAction represents a suggested fix or refactoring.
type CodeAction struct {
	Title string         `json:"title"`
	Kind  CodeActionKind `json:"kind"`
	Edits []TextEdit     `json:"edits"`
}

// TextEdit represents a text edit to apply.
type TextEdit struct {
	File    string `json:"file"`
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// CodeActions returns available code actions for the given diagnostic range.
func (e *Engine) CodeActions(path string, line, col int) []CodeAction {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	var actions []CodeAction

	// Get diagnostics for this file to find actionable ones
	diags := e.Diagnose(path)

	for _, d := range diags {
		if d.File != path || !posInRange(line, col, d.Range) {
			continue
		}

		// Quick fix: create missing connector
		if action := quickFixUndefinedConnector(d, fi, path); action != nil {
			actions = append(actions, *action)
		}

		// Quick fix: create missing type
		if action := quickFixUndefinedType(d, fi, path); action != nil {
			actions = append(actions, *action)
		}

		// Quick fix: add missing required attribute
		if action := quickFixMissingAttr(d, fi); action != nil {
			actions = append(actions, *action)
		}
	}

	return actions
}

// quickFixUndefinedConnector suggests creating a connector block.
func quickFixUndefinedConnector(d *Diagnostic, fi *FileIndex, path string) *CodeAction {
	if d.Severity != SeverityError {
		return nil
	}

	name := extractQuoted(d.Message, "undefined connector ")
	if name == "" {
		return nil
	}

	// Insert new connector block at the end of the file
	endPos := Position{Line: 1, Col: 1}
	if len(fi.Blocks) > 0 {
		lastBlock := fi.Blocks[len(fi.Blocks)-1]
		endPos = lastBlock.Range.End
		endPos.Line += 2
		endPos.Col = 1
	}

	snippet := fmt.Sprintf("\nconnector %q {\n  type = \"rest\"\n  port = 3000\n}\n", name)

	return &CodeAction{
		Title: fmt.Sprintf("Create connector %q", name),
		Kind:  CodeActionQuickFix,
		Edits: []TextEdit{{
			File:    path,
			Range:   Range{Start: endPos, End: endPos},
			NewText: snippet,
		}},
	}
}

// quickFixUndefinedType suggests creating a type block.
func quickFixUndefinedType(d *Diagnostic, fi *FileIndex, path string) *CodeAction {
	if d.Severity != SeverityWarning {
		return nil
	}

	name := extractQuoted(d.Message, "undefined type ")
	if name == "" {
		return nil
	}

	endPos := Position{Line: 1, Col: 1}
	if len(fi.Blocks) > 0 {
		lastBlock := fi.Blocks[len(fi.Blocks)-1]
		endPos = lastBlock.Range.End
		endPos.Line += 2
		endPos.Col = 1
	}

	snippet := fmt.Sprintf("\ntype %q {\n  \n}\n", name)

	return &CodeAction{
		Title: fmt.Sprintf("Create type %q", name),
		Kind:  CodeActionQuickFix,
		Edits: []TextEdit{{
			File:    path,
			Range:   Range{Start: endPos, End: endPos},
			NewText: snippet,
		}},
	}
}

// quickFixMissingAttr suggests adding a missing required attribute.
func quickFixMissingAttr(d *Diagnostic, fi *FileIndex) *CodeAction {
	if d.Severity != SeverityError {
		return nil
	}

	attrName := extractQuoted(d.Message, "missing required attribute ")
	if attrName == "" {
		return nil
	}

	// Find the block this diagnostic points to
	for _, b := range fi.Blocks {
		if posInRange(d.Range.Start.Line, d.Range.Start.Col, b.Range) {
			insertPos := b.Range.Start
			insertPos.Line++
			insertPos.Col = 3

			snippet := fmt.Sprintf("  %s = \"\"\n", attrName)

			return &CodeAction{
				Title: fmt.Sprintf("Add %s attribute", attrName),
				Kind:  CodeActionQuickFix,
				Edits: []TextEdit{{
					File:    d.File,
					Range:   Range{Start: insertPos, End: insertPos},
					NewText: snippet,
				}},
			}
		}
	}

	return nil
}

// extractQuoted extracts a quoted name from a diagnostic message.
// e.g., extractQuoted(`undefined connector "api"`, "undefined connector ") returns "api".
func extractQuoted(msg, prefix string) string {
	idx := len(prefix)
	if len(msg) <= idx || msg[:idx] != prefix {
		return ""
	}
	rest := msg[idx:]
	if len(rest) < 3 || rest[0] != '"' {
		return ""
	}
	end := 1
	for end < len(rest) && rest[end] != '"' {
		end++
	}
	if end >= len(rest) {
		return ""
	}
	return rest[1:end]
}

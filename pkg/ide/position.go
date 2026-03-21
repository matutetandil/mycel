// Package ide provides IDE intelligence for Mycel HCL configurations.
// It builds a project index from HCL files and answers queries for
// completions, diagnostics, hover documentation, and go-to-definition.
//
// This package is designed to be imported by Mycel Studio (Go + Wails)
// and has no dependency on internal/ packages. It parses HCL directly
// using hashicorp/hcl/v2 in a permissive mode that tolerates incomplete
// and invalid files.
package ide

// Position represents a location in a source file.
type Position struct {
	Line   int `json:"line"`   // 1-based
	Col    int `json:"col"`    // 1-based
	Offset int `json:"offset"` // 0-based byte offset
}

// Range represents a span in a source file.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a position in a specific file.
type Location struct {
	File  string `json:"file"`
	Range Range  `json:"range"`
}

// Severity indicates the severity of a diagnostic.
type Severity int

const (
	SeverityError   Severity = 1
	SeverityWarning Severity = 2
	SeverityInfo    Severity = 3
)

// Diagnostic represents an error, warning, or info message for a file.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	File     string   `json:"file"`
	Range    Range    `json:"range"`
}

// CompletionKind classifies a completion item.
type CompletionKind int

const (
	CompletionBlock     CompletionKind = 1
	CompletionAttribute CompletionKind = 2
	CompletionValue     CompletionKind = 3
)

// CompletionItem represents a single completion suggestion.
type CompletionItem struct {
	Label      string         `json:"label"`
	Kind       CompletionKind `json:"kind"`
	Detail     string         `json:"detail,omitempty"`
	Doc        string         `json:"doc,omitempty"`
	InsertText string         `json:"insertText,omitempty"`
}

// HoverResult contains hover documentation for a position.
type HoverResult struct {
	Content string `json:"content"`
	Range   Range  `json:"range"`
}

// CursorContext describes where the cursor is within the HCL structure.
type CursorContext struct {
	// BlockPath is the nesting path of block types, e.g. ["flow", "from"].
	BlockPath []string

	// Block is the innermost block containing the cursor. Nil if at root.
	Block *Block

	// AttrName is the attribute name if the cursor is on or after one.
	AttrName string

	// InValue is true if the cursor is in the value position (after =).
	InValue bool
}

// findCursorContext determines the cursor's position within the file structure.
func findCursorContext(fi *FileIndex, line, col int) *CursorContext {
	ctx := &CursorContext{}
	findBlock(fi.Blocks, line, col, ctx)
	return ctx
}

// findBlock recursively finds the deepest block containing the cursor.
func findBlock(blocks []*Block, line, col int, ctx *CursorContext) {
	for _, b := range blocks {
		if !posInRange(line, col, b.Range) {
			continue
		}
		ctx.BlockPath = append(ctx.BlockPath, b.Type)
		ctx.Block = b

		// Check if cursor is on an attribute
		for _, attr := range b.Attrs {
			if posInRange(line, col, attr.Range) {
				ctx.AttrName = attr.Name
				ctx.InValue = posAfterOrAt(line, col, attr.ValRange.Start)
				return
			}
		}

		// Recurse into children
		findBlock(b.Children, line, col, ctx)
		return
	}
}

// posInRange returns true if (line, col) is within the range.
func posInRange(line, col int, r Range) bool {
	if line < r.Start.Line || line > r.End.Line {
		return false
	}
	if line == r.Start.Line && col < r.Start.Col {
		return false
	}
	if line == r.End.Line && col > r.End.Col {
		return false
	}
	return true
}

// posAfterOrAt returns true if (line, col) is at or after the position.
func posAfterOrAt(line, col int, p Position) bool {
	if line > p.Line {
		return true
	}
	return line == p.Line && col >= p.Col
}

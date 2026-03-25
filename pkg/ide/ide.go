package ide

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/matutetandil/mycel/pkg/schema"
)

// Engine is the IDE intelligence engine. It indexes a Mycel project directory
// and answers queries for completions, diagnostics, hover, and go-to-definition.
// All methods are thread-safe.
type Engine struct {
	rootDir  string
	index    *ProjectIndex
	registry *schema.Registry
	mu       sync.RWMutex
}

// Option configures the engine.
type Option func(*Engine)

// WithRegistry sets a schema registry for connector-type-aware intelligence.
// When set, the engine uses connector-specific schemas from the registry
// instead of the static defaults.
func WithRegistry(reg *schema.Registry) Option {
	return func(e *Engine) {
		e.registry = reg
	}
}

// NewEngine creates an IDE engine for the given project directory.
func NewEngine(rootDir string, opts ...Option) *Engine {
	e := &Engine{
		rootDir: rootDir,
		index:   newProjectIndex(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Registry returns the engine's schema registry, or nil if none set.
func (e *Engine) Registry() *schema.Registry {
	return e.registry
}

// FullReindex scans the project directory and indexes all HCL files.
// Returns all diagnostics across the project.
func (e *Engine) FullReindex() []*Diagnostic {
	e.mu.Lock()
	e.index = newProjectIndex()
	e.mu.Unlock()

	var files []string
	_ = filepath.Walk(e.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip hidden dirs and common non-config dirs
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "mycel_plugins" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".mycel") {
			files = append(files, path)
		}
		return nil
	})

	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fi := parseHCL(path, content)
		e.index.updateFile(fi)
	}

	return e.DiagnoseAll()
}

// UpdateFile re-parses a single file and updates the index.
// Content is the current file content (may be unsaved buffer).
// Returns diagnostics for the updated file.
func (e *Engine) UpdateFile(path string, content []byte) []*Diagnostic {
	fi := parseHCL(path, content)
	e.index.updateFile(fi)
	return e.Diagnose(path)
}

// RemoveFile removes a file from the index.
// Returns any cross-reference diagnostics affected by the removal.
func (e *Engine) RemoveFile(path string) []*Diagnostic {
	e.index.removeFile(path)
	return diagnoseCrossRefs(e.index)
}

// RenameFile updates the index when a file is renamed/moved.
// The content stays the same — only the path changes.
// Returns diagnostics for the new path.
func (e *Engine) RenameFile(oldPath, newPath string) []*Diagnostic {
	e.index.mu.Lock()
	fi, ok := e.index.Files[oldPath]
	if ok {
		delete(e.index.Files, oldPath)
		fi.Path = newPath
		// Update all entity file references
		e.index.Files[newPath] = fi
		e.index.rebuild()
	}
	e.index.mu.Unlock()

	if !ok {
		return nil
	}
	return e.Diagnose(newPath)
}

// Diagnose returns diagnostics for a single file (parse + schema + cross-refs).
func (e *Engine) Diagnose(path string) []*Diagnostic {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	diags := diagnoseFile(fi, e.registry)
	diags = append(diags, diagnoseCrossRefs(e.index)...)
	return diags
}

// DiagnoseAll returns diagnostics for all files in the project.
func (e *Engine) DiagnoseAll() []*Diagnostic {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	var diags []*Diagnostic
	for _, fi := range e.index.Files {
		diags = append(diags, diagnoseFile(fi, e.registry)...)
	}

	// Cross-reference diagnostics (must release read lock first)
	e.index.mu.RUnlock()
	crossDiags := diagnoseCrossRefs(e.index)
	e.index.mu.RLock()

	diags = append(diags, crossDiags...)
	return diags
}

// Complete returns completion items for the given position.
func (e *Engine) Complete(path string, line, col int) []CompletionItem {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	return complete(fi, e.index, e.registry, line, col)
}

// Definition returns the source location of the entity referenced at the cursor.
func (e *Engine) Definition(path string, line, col int) *Location {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	ctx := findCursorContext(fi, line, col)
	if ctx.Block == nil || !ctx.InValue || ctx.AttrName == "" {
		return nil
	}

	// Find the attribute value
	var value string
	for _, attr := range ctx.Block.Attrs {
		if attr.Name == ctx.AttrName {
			value = attr.ValueRaw
			break
		}
	}
	if value == "" {
		return nil
	}

	// Determine what kind of reference this is
	schema := lookupBlockSchema(ctx.BlockPath)
	if schema == nil {
		return nil
	}
	as := findAttrSchema(schema.Attrs, ctx.AttrName)
	if as == nil || as.Ref == RefNone {
		return nil
	}

	// Resolve the reference
	var entity *NamedEntity
	switch as.Ref {
	case RefConnector:
		entity = e.index.lookupEntity("connector", value)
	case RefType:
		name := value
		if len(name) > 5 && name[:5] == "type." {
			name = name[5:]
		}
		entity = e.index.lookupEntity("type", name)
	case RefTransform:
		entity = e.index.lookupEntity("transform", value)
	case RefFlow:
		entity = e.index.lookupEntity("flow", value)
	case RefCache:
		name := value
		if len(name) > 6 && name[:6] == "cache." {
			name = name[6:]
		}
		entity = e.index.lookupEntity("cache", name)
	}

	if entity == nil {
		return nil
	}

	return &Location{
		File:  entity.File,
		Range: entity.Range,
	}
}

// Hover returns documentation for the element at the cursor.
func (e *Engine) Hover(path string, line, col int) *HoverResult {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	ctx := findCursorContext(fi, line, col)

	// Hovering over an attribute name — show attribute doc
	if ctx.AttrName != "" && !ctx.InValue {
		schema := lookupBlockSchema(ctx.BlockPath)
		if schema != nil {
			as := findAttrSchema(schema.Attrs, ctx.AttrName)
			if as != nil {
				content := as.Doc
				if len(as.Values) > 0 {
					content += "\n\nValid values: " + strings.Join(as.Values, ", ")
				}
				return &HoverResult{Content: content}
			}
		}
	}

	// Hovering over a reference value — show target entity info
	if ctx.InValue && ctx.AttrName != "" && ctx.Block != nil {
		var value string
		for _, attr := range ctx.Block.Attrs {
			if attr.Name == ctx.AttrName {
				value = attr.ValueRaw
				break
			}
		}
		if value != "" {
			schema := lookupBlockSchema(ctx.BlockPath)
			if schema != nil {
				as := findAttrSchema(schema.Attrs, ctx.AttrName)
				if as != nil && as.Ref == RefConnector {
					entity := e.index.lookupEntity("connector", value)
					if entity != nil {
						content := "Connector: " + entity.Name
						if entity.ConnType != "" {
							content += "\nType: " + entity.ConnType
						}
						if entity.Driver != "" {
							content += "\nDriver: " + entity.Driver
						}
						content += "\nDefined in: " + entity.File
						return &HoverResult{Content: content}
					}
				}
			}
		}
	}

	// Hovering over a block type keyword — show block doc
	if ctx.Block != nil && ctx.AttrName == "" {
		blockType := ctx.Block.Type
		if s := findBlockSchemaByType(blockType); s != nil {
			return &HoverResult{Content: s.Doc}
		}
	}

	return nil
}

// RemoveBlock returns a TextEdit that removes a named block from a file.
// The edit removes the block and any surrounding blank lines to keep formatting clean.
// blockType is "connector", "flow", "type", etc. name is the block label.
func (e *Engine) RemoveBlock(path, blockType, name string) *TextEdit {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	for _, b := range fi.Blocks {
		if b.Type == blockType && b.Name == name {
			// Expand range to include the line before (if blank) and line after (if blank)
			startLine := b.Range.Start.Line
			endLine := b.Range.End.Line

			return &TextEdit{
				File: path,
				Range: Range{
					Start: Position{Line: startLine, Col: 1},
					End:   Position{Line: endLine + 1, Col: 1}, // include trailing newline
				},
				NewText: "",
			}
		}
	}

	return nil
}

// GetIndex returns the current project index (for testing and inspection).
func (e *Engine) GetIndex() *ProjectIndex {
	return e.index
}

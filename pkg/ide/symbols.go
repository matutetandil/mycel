package ide

// SymbolKind classifies a workspace symbol.
type SymbolKind int

const (
	SymbolConnector    SymbolKind = 1
	SymbolFlow         SymbolKind = 2
	SymbolType         SymbolKind = 3
	SymbolTransform    SymbolKind = 4
	SymbolAspect       SymbolKind = 5
	SymbolValidator    SymbolKind = 6
	SymbolCache        SymbolKind = 7
	SymbolSaga         SymbolKind = 8
	SymbolStateMachine SymbolKind = 9
)

// Symbol represents a named entity in the workspace.
type Symbol struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	KindName string     `json:"kindName"`
	Detail   string     `json:"detail,omitempty"`
	File     string     `json:"file"`
	Range    Range      `json:"range"`
}

// Symbols returns all named entities in the project for workspace navigation (Ctrl+P).
func (e *Engine) Symbols() []Symbol {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	var symbols []Symbol

	for _, entity := range e.index.Connectors {
		detail := entity.ConnType
		if entity.Driver != "" {
			detail += "/" + entity.Driver
		}
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolConnector,
			KindName: "connector",
			Detail:   detail,
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Flows {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolFlow,
			KindName: "flow",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Types {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolType,
			KindName: "type",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Transforms {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolTransform,
			KindName: "transform",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Aspects {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolAspect,
			KindName: "aspect",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Validators {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolValidator,
			KindName: "validator",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Caches {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolCache,
			KindName: "cache",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.Sagas {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolSaga,
			KindName: "saga",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	for _, entity := range e.index.StateMachines {
		symbols = append(symbols, Symbol{
			Name:     entity.Name,
			Kind:     SymbolStateMachine,
			KindName: "state_machine",
			File:     entity.File,
			Range:    entity.Range,
		})
	}

	return symbols
}

// SymbolsForFile returns symbols defined in a specific file (for document outline).
func (e *Engine) SymbolsForFile(path string) []Symbol {
	e.index.mu.RLock()
	fi, ok := e.index.Files[path]
	e.index.mu.RUnlock()

	if !ok {
		return nil
	}

	var symbols []Symbol
	for _, b := range fi.Blocks {
		if b.Name == "" {
			continue
		}
		s := Symbol{
			Name:     b.Name,
			KindName: b.Type,
			File:     path,
			Range:    b.Range,
		}
		switch b.Type {
		case "connector":
			s.Kind = SymbolConnector
			detail := b.GetAttr("type")
			if d := b.GetAttr("driver"); d != "" {
				detail += "/" + d
			}
			s.Detail = detail
		case "flow":
			s.Kind = SymbolFlow
		case "type":
			s.Kind = SymbolType
		case "transform":
			s.Kind = SymbolTransform
		case "aspect":
			s.Kind = SymbolAspect
		case "validator":
			s.Kind = SymbolValidator
		case "cache":
			s.Kind = SymbolCache
		case "saga":
			s.Kind = SymbolSaga
		case "state_machine":
			s.Kind = SymbolStateMachine
		}
		symbols = append(symbols, s)
	}

	return symbols
}

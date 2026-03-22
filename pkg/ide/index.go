package ide

import "sync"

// Block represents a parsed HCL block with position information.
type Block struct {
	Type     string       `json:"type"`
	Name     string       `json:"name,omitempty"`
	Range    Range        `json:"range"`
	Attrs    []*Attribute `json:"attrs,omitempty"`
	Children []*Block     `json:"children,omitempty"`
}

// GetAttr returns the raw value of an attribute, or empty string if not found.
func (b *Block) GetAttr(name string) string {
	for _, a := range b.Attrs {
		if a.Name == name {
			return a.ValueRaw
		}
	}
	return ""
}

// HasAttr returns true if the attribute exists in the block (even if its value is dynamic/unresolvable).
func (b *Block) HasAttr(name string) bool {
	for _, a := range b.Attrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// Attribute represents a parsed HCL attribute with position information.
type Attribute struct {
	Name     string `json:"name"`
	ValueRaw string `json:"valueRaw"`
	Range    Range  `json:"range"`
	ValRange Range  `json:"valRange"`
}

// FileIndex holds all parsed data from a single HCL file.
type FileIndex struct {
	Path       string        `json:"path"`
	Blocks     []*Block      `json:"blocks"`
	ParseDiags []*Diagnostic `json:"parseDiags,omitempty"`
}

// NamedEntity represents a named element defined in the project.
type NamedEntity struct {
	Kind     string `json:"kind"`     // "connector", "flow", "type", "transform", etc.
	Name     string `json:"name"`     // block label
	File     string `json:"file"`     // source file path
	Range    Range  `json:"range"`    // block range for go-to-definition
	ConnType string `json:"connType"` // connector type (rest, database, mq, etc.)
	Driver   string `json:"driver"`   // connector driver (sqlite, postgres, rabbitmq, etc.)
}

// ProjectIndex holds the aggregated index of all HCL files in a project.
type ProjectIndex struct {
	mu    sync.RWMutex
	Files map[string]*FileIndex

	// Aggregated lookup tables
	Connectors    map[string]*NamedEntity
	Flows         map[string]*NamedEntity
	Types         map[string]*NamedEntity
	Transforms    map[string]*NamedEntity
	Aspects       map[string]*NamedEntity
	Validators    map[string]*NamedEntity
	Caches        map[string]*NamedEntity
	Sagas         map[string]*NamedEntity
	StateMachines map[string]*NamedEntity
}

// newProjectIndex creates an empty project index.
func newProjectIndex() *ProjectIndex {
	return &ProjectIndex{
		Files:         make(map[string]*FileIndex),
		Connectors:    make(map[string]*NamedEntity),
		Flows:         make(map[string]*NamedEntity),
		Types:         make(map[string]*NamedEntity),
		Transforms:    make(map[string]*NamedEntity),
		Aspects:       make(map[string]*NamedEntity),
		Validators:    make(map[string]*NamedEntity),
		Caches:        make(map[string]*NamedEntity),
		Sagas:         make(map[string]*NamedEntity),
		StateMachines: make(map[string]*NamedEntity),
	}
}

// updateFile replaces the index for a single file and rebuilds lookup tables.
func (idx *ProjectIndex) updateFile(fi *FileIndex) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.Files[fi.Path] = fi
	idx.rebuild()
}

// removeFile removes a file from the index and rebuilds lookup tables.
func (idx *ProjectIndex) removeFile(path string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.Files, path)
	idx.rebuild()
}

// rebuild regenerates all lookup tables from the current file set.
// Must be called with mu held.
func (idx *ProjectIndex) rebuild() {
	idx.Connectors = make(map[string]*NamedEntity)
	idx.Flows = make(map[string]*NamedEntity)
	idx.Types = make(map[string]*NamedEntity)
	idx.Transforms = make(map[string]*NamedEntity)
	idx.Aspects = make(map[string]*NamedEntity)
	idx.Validators = make(map[string]*NamedEntity)
	idx.Caches = make(map[string]*NamedEntity)
	idx.Sagas = make(map[string]*NamedEntity)
	idx.StateMachines = make(map[string]*NamedEntity)

	for _, fi := range idx.Files {
		for _, b := range fi.Blocks {
			entity := &NamedEntity{
				Kind:  b.Type,
				Name:  b.Name,
				File:  fi.Path,
				Range: b.Range,
			}

			switch b.Type {
			case "connector":
				entity.ConnType = b.GetAttr("type")
				entity.Driver = b.GetAttr("driver")
				idx.Connectors[b.Name] = entity
			case "flow":
				idx.Flows[b.Name] = entity
			case "type":
				idx.Types[b.Name] = entity
			case "transform":
				idx.Transforms[b.Name] = entity
			case "aspect":
				idx.Aspects[b.Name] = entity
			case "validator":
				idx.Validators[b.Name] = entity
			case "cache":
				idx.Caches[b.Name] = entity
			case "saga":
				idx.Sagas[b.Name] = entity
			case "state_machine":
				idx.StateMachines[b.Name] = entity
			}
		}
	}
}

// lookupEntity finds a named entity across all entity types.
func (idx *ProjectIndex) lookupEntity(kind, name string) *NamedEntity {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

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

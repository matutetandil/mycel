// Package schema provides the canonical schema definitions for Mycel HCL configurations.
// Every HCL block type in Mycel is described by a Block struct that declares its valid
// attributes, child blocks, and documentation. This is the single source of truth used by:
//   - The parser (internal/parser/) for validation
//   - The IDE engine (pkg/ide/) for completions, diagnostics, and hover
//   - Connector implementations for self-description
//
// This package has zero dependencies on internal/ packages.
package schema

// AttrType describes the expected type of an attribute value.
type AttrType string

const (
	TypeString   AttrType = "string"
	TypeNumber   AttrType = "number"
	TypeBool     AttrType = "bool"
	TypeMap      AttrType = "map"
	TypeList     AttrType = "list"
	TypeDuration AttrType = "duration" // string parsed as duration (e.g., "5s", "1h")
)

// RefKind indicates what kind of entity an attribute references.
type RefKind int

const (
	RefNone RefKind = iota
	RefConnector
	RefType
	RefTransform
	RefCache
	RefValidator
	RefFlow
	RefStateMachine
)

// Attr describes a single attribute in a block.
type Attr struct {
	// Name is the attribute key in HCL.
	Name string

	// Doc is a human-readable description for hover and completions.
	Doc string

	// Type is the expected value type.
	Type AttrType

	// Required indicates the attribute must be present.
	Required bool

	// Values lists the valid enum values (if any).
	// Empty means any value of the correct type is accepted.
	Values []string

	// Ref indicates this attribute references another named entity.
	Ref RefKind

	// Default is the default value when not specified. Nil means no default.
	Default interface{}
}

// Block describes the structure of an HCL block type.
type Block struct {
	// Type is the block type keyword (e.g., "connector", "flow", "from").
	Type string

	// Doc is a human-readable description for hover and completions.
	Doc string

	// Labels is the number of labels the block requires (0 or 1).
	// Example: `flow "name" {}` has 1 label, `service {}` has 0.
	Labels int

	// Open indicates the block accepts arbitrary attributes beyond those declared.
	// Used for dynamic blocks like transform (CEL mappings), type (field definitions),
	// and from/to/step (connector-specific params).
	Open bool

	// Attrs lists the known attributes for this block.
	Attrs []Attr

	// Children lists the valid nested block types.
	Children []Block
}

// SchemaProvider is implemented by any element that can describe its HCL schema.
type SchemaProvider interface {
	Schema() Block
}

// ConnectorSchemaProvider is implemented by connectors that describe their full schema.
// Each connector knows its own attributes, child blocks (pool, consumer, tls, etc.),
// and what params are valid when used as a source (from) or target (to/step).
type ConnectorSchemaProvider interface {
	// ConnectorSchema returns the connector-level block schema.
	// Includes all attributes and nested blocks the connector accepts
	// (e.g., host, port, pool {}, consumer {}, tls {}).
	ConnectorSchema() Block

	// SourceSchema returns additional attributes valid in a flow "from" block
	// when this connector is the source. Returns nil if not a valid source.
	SourceSchema() *Block

	// TargetSchema returns additional attributes valid in a flow "to"/"step" block
	// when this connector is the target. Returns nil if not a valid target.
	TargetSchema() *Block
}

// Merge overlays additional attributes and children onto a base block.
// Used to combine a base connector schema with type-specific additions.
func Merge(base, overlay Block) Block {
	merged := base

	// Merge attrs (overlay wins on name collision)
	existing := make(map[string]bool)
	for _, a := range merged.Attrs {
		existing[a.Name] = true
	}
	for _, a := range overlay.Attrs {
		if !existing[a.Name] {
			merged.Attrs = append(merged.Attrs, a)
		}
	}

	// Merge children (overlay wins on type collision)
	existingChildren := make(map[string]bool)
	for _, c := range merged.Children {
		existingChildren[c.Type] = true
	}
	for _, c := range overlay.Children {
		if !existingChildren[c.Type] {
			merged.Children = append(merged.Children, c)
		}
	}

	// Inherit doc from overlay if base is empty
	if merged.Doc == "" && overlay.Doc != "" {
		merged.Doc = overlay.Doc
	}

	return merged
}

// HasAttr returns true if the block declares an attribute with the given name.
func (b *Block) HasAttr(name string) bool {
	for _, a := range b.Attrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// GetAttr returns the attribute with the given name, or nil.
func (b *Block) GetAttr(name string) *Attr {
	for i := range b.Attrs {
		if b.Attrs[i].Name == name {
			return &b.Attrs[i]
		}
	}
	return nil
}

// FindChild returns the child block schema with the given type, or nil.
func (b *Block) FindChild(blockType string) *Block {
	for i := range b.Children {
		if b.Children[i].Type == blockType {
			return &b.Children[i]
		}
	}
	return nil
}

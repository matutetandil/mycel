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
	// Reusable inline-block references (v2.6): use = "<kind>.<name>".
	RefDedupe
	RefRetry
	RefLock
	RefSemaphore
	RefSequenceGuard
	RefCoordinate
	RefTransaction
	RefErrorHandling
	RefAccept
	RefResponse
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

	// RequiredOneOf lists groups of attributes; at least one attribute from
	// each group must be present.
	//
	// Some rules are not "this attribute is required" but "say it one way or
	// the other". A profiled connector names the profile to use with `select`
	// or with `default`. A Postgres connector needs a database name, which is
	// written either as `database` or inside `url` — the URL is taken apart
	// before anything is checked, so both end up in the same place.
	//
	// Marking every alternative Required describes a rule that does not exist
	// and makes the generator write a file nobody wants; marking none of them
	// leaves the rule invisible to everything except the connector, which
	// reports it at start-up rather than at `mycel validate`.
	RequiredOneOf [][]string

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

	// Merge attrs. The overlay wins on a name collision: the base describes
	// what every connector has, the overlay what this one makes of it, and the
	// specific description is the true one.
	//
	// It used to be the other way round — the base was kept and the overlay's
	// version dropped — while the comment said what it says now. Nothing looked
	// merged-in wrong, it simply went missing: every driver list a connector
	// declared (memory or redis for cache, twilio or sns for sms, four for
	// database) was discarded in favour of the base's bare "driver" with no
	// values and nothing required. So the check that exists to catch a misspelt
	// word never saw a driver at all, and `driver = "postgress"` validated.
	merged.Attrs = append([]Attr(nil), merged.Attrs...)
	position := make(map[string]int, len(merged.Attrs))
	for i, a := range merged.Attrs {
		position[a.Name] = i
	}
	for _, a := range overlay.Attrs {
		if i, clash := position[a.Name]; clash {
			merged.Attrs[i] = a
			continue
		}
		position[a.Name] = len(merged.Attrs)
		merged.Attrs = append(merged.Attrs, a)
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

	// Carry the overlay's "say it one way or the other" rules. These belong to
	// the connector rather than to connectors in general, so the base has none
	// and dropping them here made the merged schema describe a rule nobody
	// had to answer.
	if len(overlay.RequiredOneOf) > 0 {
		merged.RequiredOneOf = append(append([][]string(nil), merged.RequiredOneOf...), overlay.RequiredOneOf...)
	}

	// A block that takes attributes nobody declared says so, and the overlay
	// is the one that knows.
	if overlay.Open {
		merged.Open = true
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

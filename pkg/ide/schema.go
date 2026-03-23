package ide

import "github.com/matutetandil/mycel/pkg/schema"

// Type aliases — pkg/ide uses the same types as pkg/schema.
// This avoids breaking the existing public API while delegating to the canonical source.
type AttrType = schema.AttrType

const (
	AttrString   = schema.TypeString
	AttrNumber   = schema.TypeNumber
	AttrBool     = schema.TypeBool
	AttrMap      = schema.TypeMap
	AttrList     = schema.TypeList
	AttrDuration = schema.TypeDuration
)

type RefKind = schema.RefKind

const (
	RefNone         = schema.RefNone
	RefConnector    = schema.RefConnector
	RefType         = schema.RefType
	RefTransform    = schema.RefTransform
	RefCache        = schema.RefCache
	RefValidator    = schema.RefValidator
	RefFlow         = schema.RefFlow
	RefStateMachine = schema.RefStateMachine
)

// BlockSchema is an alias for schema.Block.
type BlockSchema = schema.Block

// AttrSchema is an alias for schema.Attr.
type AttrSchema = schema.Attr

// rootSchema returns the schema for all top-level blocks.
// Delegates to pkg/schema as the single source of truth.
func rootSchema() []BlockSchema {
	return schema.BuiltinRootSchemas()
}

// lookupBlockSchema finds the schema for a block at the given path.
// For example, path ["flow", "from"] returns the fromSchema.
func lookupBlockSchema(path []string) *BlockSchema {
	schemas := rootSchema()
	var current *BlockSchema

	for _, segment := range path {
		found := false
		for i := range schemas {
			if schemas[i].Type == segment {
				current = &schemas[i]
				schemas = current.Children
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return current
}

// validRootBlockTypes returns the type names of all valid root-level blocks.
func validRootBlockTypes() []string {
	schemas := rootSchema()
	types := make([]string, len(schemas))
	for i, s := range schemas {
		types[i] = s.Type
	}
	return types
}

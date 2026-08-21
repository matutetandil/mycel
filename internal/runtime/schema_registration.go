package runtime

import (
	"github.com/matutetandil/mycel/v3/pkg/connectors"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// RegisterBuiltinSchemas populates a schema registry with all built-in
// connector schemas. Exported so Studio and CLI can use it:
//
//	reg := schema.NewRegistryWith(runtime.RegisterBuiltinSchemas)
//
// The schemas themselves live in pkg/connectors, one file per connector. This
// used to be a second copy of that list, with a second copy of every schema
// behind it, and the two drifted for three releases without anything noticing:
// create_if_missing, added to the queue connectors in 2.0.0, and Slack's batch
// block from 2.5.0 were only ever visible to the runtime, while the copy
// external tooling reads described neither.
func RegisterBuiltinSchemas(reg *schema.Registry) {
	connectors.RegisterAll(reg)
}

// NewSchemaRegistry creates a fully-populated schema registry with all
// built-in block schemas and connector schemas. This is the single entry
// point for consumers that need the complete Mycel schema.
func NewSchemaRegistry() *schema.Registry {
	return schema.NewRegistryWith(RegisterBuiltinSchemas)
}

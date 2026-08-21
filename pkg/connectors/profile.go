package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// ProfileSchema implements ConnectorSchemaProvider for profiled connectors.
//
// A profiled connector is one name that resolves to a different backend
// depending on where it is running or what a message says: the same `pricing`
// connector reads from a vendor's API in one environment and from a database
// in another, and a flow that names it does not change.
//
// It is the only connector type that had no schema at all — documented, with
// an example, in the parser's list of built-in types, and unknown to
// completions, `mycel add` and validation. Asking for one produced "unknown
// connector type".
//
// The profiles themselves carry the type: each `profile` block is a whole
// connector configuration, which is why the block is open rather than a second
// copy of every connector's attributes that would go stale the day one changed.
type ProfileSchema struct{}

func (ProfileSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Doc:           "One connector name that resolves to a different backend per environment or per message",
		RequiredOneOf: [][]string{{"default", "select"}},
		Attrs: []schema.Attr{
			{Name: "select", Doc: "CEL expression naming the profile to use, e.g. env('PRICE_SOURCE')", Type: schema.TypeString},
			{Name: "default", Doc: "Profile used when select names none — one of select or default is required", Type: schema.TypeString},
			{Name: "fallback", Doc: "Profiles to try, in order, when the selected one fails", Type: schema.TypeList},
		},
		Children: []schema.Block{
			{
				Type:   "profile",
				Doc:    "One backend this connector can resolve to; takes everything a connector takes, plus a transform",
				Labels: 1,
				Open:   true,
				Attrs: []schema.Attr{
					{Name: "type", Doc: "Connector type for this profile", Type: schema.TypeString, Required: true, Values: schema.ConnectorTypeNames()},
					{Name: "driver", Doc: "Driver for this profile's type", Type: schema.TypeString},
				},
				Children: []schema.Block{
					{
						Type: "transform",
						Doc:  "Reshape this backend's payload so every profile answers the same way",
						Open: true,
					},
				},
			},
		},
	}
}

// A profiled connector is read from and written to through whichever profile
// was selected, so what a flow may write in a from, to or step block is the
// selected connector's business rather than this one's.
func (ProfileSchema) SourceSchema() *schema.Block { return nil }
func (ProfileSchema) TargetSchema() *schema.Block { return nil }

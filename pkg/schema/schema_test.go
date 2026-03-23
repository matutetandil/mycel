package schema

import "testing"

func TestBuiltinRootSchemas(t *testing.T) {
	schemas := BuiltinRootSchemas()
	if len(schemas) < 16 {
		t.Errorf("expected at least 16 root schemas, got %d", len(schemas))
	}

	// Verify key block types exist
	types := make(map[string]bool)
	for _, s := range schemas {
		types[s.Type] = true
	}

	expected := []string{"connector", "flow", "type", "transform", "aspect", "service",
		"validator", "saga", "state_machine", "functions", "plugin", "auth",
		"security", "mocks", "cache", "environment"}
	for _, e := range expected {
		if !types[e] {
			t.Errorf("missing root block type %q", e)
		}
	}
}

func TestFlowSchema(t *testing.T) {
	flow := FlowSchema()
	if flow.Type != "flow" {
		t.Errorf("expected type=flow, got %s", flow.Type)
	}
	if flow.Labels != 1 {
		t.Error("flow should require 1 label")
	}

	// Check key children exist
	childTypes := make(map[string]bool)
	for _, c := range flow.Children {
		childTypes[c.Type] = true
	}

	expected := []string{"from", "to", "accept", "step", "transform", "response",
		"validate", "enrich", "lock", "semaphore", "error_handling", "dedupe"}
	for _, e := range expected {
		if !childTypes[e] {
			t.Errorf("flow missing child block %q", e)
		}
	}
}

func TestAcceptSchema(t *testing.T) {
	accept := AcceptSchema()
	if accept.Open {
		t.Error("accept should be strict (not open)")
	}

	when := accept.GetAttr("when")
	if when == nil {
		t.Fatal("accept missing 'when' attribute")
	}
	if !when.Required {
		t.Error("accept.when should be required")
	}

	onReject := accept.GetAttr("on_reject")
	if onReject == nil {
		t.Fatal("accept missing 'on_reject' attribute")
	}
	if len(onReject.Values) != 3 {
		t.Errorf("expected 3 on_reject values, got %d", len(onReject.Values))
	}
}

func TestMerge(t *testing.T) {
	base := Block{
		Type: "connector",
		Attrs: []Attr{
			{Name: "type", Type: TypeString, Required: true},
		},
		Children: []Block{
			{Type: "tls", Doc: "TLS"},
		},
	}

	overlay := Block{
		Attrs: []Attr{
			{Name: "host", Type: TypeString},
			{Name: "port", Type: TypeNumber},
			{Name: "type", Type: TypeString}, // duplicate — should not be added twice
		},
		Children: []Block{
			{Type: "pool", Doc: "Pool"},
			{Type: "tls", Doc: "TLS v2"}, // duplicate — should not be added twice
		},
	}

	merged := Merge(base, overlay)

	if len(merged.Attrs) != 3 {
		t.Errorf("expected 3 attrs after merge (type + host + port), got %d", len(merged.Attrs))
	}
	if len(merged.Children) != 2 {
		t.Errorf("expected 2 children after merge (tls + pool), got %d", len(merged.Children))
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	// Register a mock connector
	mock := &mockProvider{
		connSchema: Block{
			Attrs: []Attr{
				{Name: "host", Type: TypeString, Required: true},
				{Name: "port", Type: TypeNumber},
			},
			Children: []Block{
				{Type: "pool", Doc: "Connection pool"},
			},
		},
	}

	reg.Register("database", "", mock)
	reg.Register("database", "postgres", mock)

	// Lookup by type
	p := reg.Lookup("database", "")
	if p == nil {
		t.Fatal("expected provider for database")
	}

	// Lookup by type:driver
	p = reg.Lookup("database", "postgres")
	if p == nil {
		t.Fatal("expected provider for database:postgres")
	}

	// Lookup non-existent
	p = reg.Lookup("nonexistent", "")
	if p != nil {
		t.Error("expected nil for nonexistent type")
	}

	// ConnectorSchema merge
	full := reg.ConnectorSchema("database", "postgres")
	if !full.HasAttr("type") {
		t.Error("merged schema should have base 'type' attr")
	}
	if !full.HasAttr("host") {
		t.Error("merged schema should have provider 'host' attr")
	}

	// AllConnectorTypes
	types := reg.AllConnectorTypes()
	if len(types) != 1 || types[0] != "database" {
		t.Errorf("expected [database], got %v", types)
	}
}

func TestBlockHelpers(t *testing.T) {
	b := Block{
		Attrs: []Attr{
			{Name: "host", Type: TypeString},
			{Name: "port", Type: TypeNumber},
		},
		Children: []Block{
			{Type: "pool"},
			{Type: "tls"},
		},
	}

	if !b.HasAttr("host") {
		t.Error("expected HasAttr(host) = true")
	}
	if b.HasAttr("missing") {
		t.Error("expected HasAttr(missing) = false")
	}

	if b.GetAttr("port") == nil {
		t.Error("expected GetAttr(port) != nil")
	}
	if b.GetAttr("missing") != nil {
		t.Error("expected GetAttr(missing) = nil")
	}

	if b.FindChild("pool") == nil {
		t.Error("expected FindChild(pool) != nil")
	}
	if b.FindChild("missing") != nil {
		t.Error("expected FindChild(missing) = nil")
	}
}

func TestValidateParams(t *testing.T) {
	block := &Block{
		Attrs: []Attr{
			{Name: "operation", Type: TypeString, Required: true, Doc: "Operation name"},
			{Name: "format", Type: TypeString},
		},
	}

	// Missing required attr without default → error
	params := map[string]interface{}{"format": "json"}
	err := ValidateParams(block, params)
	if err == nil {
		t.Error("expected error for missing required 'operation'")
	}

	// With required attr present → no error
	params = map[string]interface{}{"operation": "GET /users"}
	err = ValidateParams(block, params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateParamsWithDefault(t *testing.T) {
	block := &Block{
		Attrs: []Attr{
			{Name: "operation", Type: TypeString, Required: false, Default: "*", Doc: "Operation"},
		},
	}

	// Missing attr with default → default applied, no error
	params := map[string]interface{}{}
	err := ValidateParams(block, params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if params["operation"] != "*" {
		t.Errorf("expected default '*' applied, got %v", params["operation"])
	}
}

// mockProvider implements ConnectorSchemaProvider for testing.
type mockProvider struct {
	connSchema Block
}

func (m *mockProvider) ConnectorSchema() Block   { return m.connSchema }
func (m *mockProvider) SourceSchema() *Block      { return nil }
func (m *mockProvider) TargetSchema() *Block      { return nil }

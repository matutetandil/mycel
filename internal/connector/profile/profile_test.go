package profile

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

// mockConnector implements the connector interfaces for testing.
type mockConnector struct {
	name       string
	connType   string
	readResult *connector.Result
	readErr    error
	writeResult *connector.Result
	writeErr   error
	connected  bool
	healthy    bool
}

func (m *mockConnector) Name() string                        { return m.name }
func (m *mockConnector) Type() string                        { return m.connType }
func (m *mockConnector) Connect(ctx context.Context) error   { m.connected = true; return nil }
func (m *mockConnector) Close(ctx context.Context) error     { m.connected = false; return nil }
func (m *mockConnector) Health(ctx context.Context) error    { if !m.healthy { return context.DeadlineExceeded }; return nil }
func (m *mockConnector) Read(ctx context.Context, q connector.Query) (*connector.Result, error) {
	return m.readResult, m.readErr
}
func (m *mockConnector) Write(ctx context.Context, d *connector.Data) (*connector.Result, error) {
	return m.writeResult, m.writeErr
}

func TestConfig_HasProfiles(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name: "no profiles",
			config: &Config{
				Profiles: nil,
			},
			want: false,
		},
		{
			name: "empty profiles",
			config: &Config{
				Profiles: make(map[string]*ProfileDef),
			},
			want: false,
		},
		{
			name: "has profiles",
			config: &Config{
				Profiles: map[string]*ProfileDef{
					"test": {Name: "test"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasProfiles(); got != tt.want {
				t.Errorf("HasProfiles() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_GetProfile(t *testing.T) {
	config := &Config{
		Profiles: map[string]*ProfileDef{
			"magento": {Name: "magento"},
			"erp":     {Name: "erp"},
		},
	}

	// Test getting existing profile
	profile, ok := config.GetProfile("magento")
	if !ok {
		t.Error("expected to find magento profile")
	}
	if profile.Name != "magento" {
		t.Errorf("expected profile name magento, got %s", profile.Name)
	}

	// Test getting non-existent profile
	_, ok = config.GetProfile("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent profile")
	}
}

func TestProfiledConnector_New(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError bool
	}{
		{
			name: "valid config with select",
			config: &Config{
				Select: "env('PROFILE')",
				Profiles: map[string]*ProfileDef{
					"test": {Name: "test"},
				},
			},
			wantError: false,
		},
		{
			name: "valid config with default",
			config: &Config{
				Default: "test",
				Profiles: map[string]*ProfileDef{
					"test": {Name: "test"},
				},
			},
			wantError: false,
		},
		{
			name: "invalid config - no select or default",
			config: &Config{
				Profiles: map[string]*ProfileDef{
					"test": {Name: "test"},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New("test", tt.config, func(cfg *connector.Config) (connector.Connector, error) {
				return &mockConnector{name: cfg.Name, healthy: true}, nil
			})

			if tt.wantError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestProfiledConnector_ResolveActiveProfile(t *testing.T) {
	// Test with default only
	config := &Config{
		Default: "erp",
		Profiles: map[string]*ProfileDef{
			"magento": {Name: "magento"},
			"erp":     {Name: "erp"},
		},
	}

	pc, err := New("test", config, func(cfg *connector.Config) (connector.Connector, error) {
		return &mockConnector{name: cfg.Name, healthy: true}, nil
	})
	if err != nil {
		t.Fatalf("failed to create profiled connector: %v", err)
	}

	profile, err := pc.resolveActiveProfile()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if profile != "erp" {
		t.Errorf("expected profile erp, got %s", profile)
	}
}

func TestProfiledConnector_GetProfileOrder(t *testing.T) {
	config := &Config{
		Default:  "magento",
		Fallback: []string{"erp", "legacy"},
		Profiles: map[string]*ProfileDef{
			"magento": {Name: "magento"},
			"erp":     {Name: "erp"},
			"legacy":  {Name: "legacy"},
		},
	}

	pc, err := New("test", config, func(cfg *connector.Config) (connector.Connector, error) {
		return &mockConnector{name: cfg.Name, healthy: true}, nil
	})
	if err != nil {
		t.Fatalf("failed to create profiled connector: %v", err)
	}

	order := pc.getProfileOrder()

	if len(order) != 3 {
		t.Errorf("expected 3 profiles in order, got %d", len(order))
	}
	if order[0] != "magento" {
		t.Errorf("expected first profile to be magento, got %s", order[0])
	}
	if order[1] != "erp" {
		t.Errorf("expected second profile to be erp, got %s", order[1])
	}
	if order[2] != "legacy" {
		t.Errorf("expected third profile to be legacy, got %s", order[2])
	}
}

func TestProfiledConnector_ApplyTransform(t *testing.T) {
	config := &Config{
		Default: "spanish",
		Profiles: map[string]*ProfileDef{
			"spanish": {
				Name: "spanish",
				ConnectorConfig: &connector.Config{
					Name: "spanish",
					Type: "database",
				},
				Transform: map[string]string{
					"price":    "input.precio",
					"name":     "input.nombre",
					"currency": "'ARS'",
				},
			},
		},
	}

	pc, err := New("test", config, func(cfg *connector.Config) (connector.Connector, error) {
		return &mockConnector{name: cfg.Name, healthy: true}, nil
	})
	if err != nil {
		t.Fatalf("failed to create profiled connector: %v", err)
	}

	// Test transform
	input := map[string]interface{}{
		"precio": 100.50,
		"nombre": "Producto Test",
	}

	transformed, err := pc.applyTransformToMap(input, "spanish")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if transformed["price"] != 100.50 {
		t.Errorf("expected price 100.50, got %v", transformed["price"])
	}
	if transformed["name"] != "Producto Test" {
		t.Errorf("expected name 'Producto Test', got %v", transformed["name"])
	}
	if transformed["currency"] != "ARS" {
		t.Errorf("expected currency 'ARS', got %v", transformed["currency"])
	}
}

func TestProfiledConnector_Stats(t *testing.T) {
	config := &Config{
		Default:  "primary",
		Fallback: []string{"secondary"},
		Profiles: map[string]*ProfileDef{
			"primary":   {Name: "primary"},
			"secondary": {Name: "secondary"},
		},
	}

	pc, err := New("test", config, func(cfg *connector.Config) (connector.Connector, error) {
		return &mockConnector{name: cfg.Name, healthy: true}, nil
	})
	if err != nil {
		t.Fatalf("failed to create profiled connector: %v", err)
	}

	stats := pc.Stats()

	if stats["active_profile"] != "primary" {
		t.Errorf("expected active_profile primary, got %v", stats["active_profile"])
	}

	profiles := stats["profiles"].([]string)
	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}

	fallback := stats["fallback"].([]string)
	if len(fallback) != 1 || fallback[0] != "secondary" {
		t.Errorf("expected fallback [secondary], got %v", fallback)
	}
}

func TestProfiledConnector_Name(t *testing.T) {
	config := &Config{
		Default: "test",
		Profiles: map[string]*ProfileDef{
			"test": {Name: "test"},
		},
	}

	pc, _ := New("myconnector", config, func(cfg *connector.Config) (connector.Connector, error) {
		return &mockConnector{name: cfg.Name, healthy: true}, nil
	})

	if pc.Name() != "myconnector" {
		t.Errorf("expected name myconnector, got %s", pc.Name())
	}
}

func TestProfiledConnector_Type(t *testing.T) {
	config := &Config{
		Default: "test",
		Profiles: map[string]*ProfileDef{
			"test": {Name: "test"},
		},
	}

	pc, _ := New("test", config, func(cfg *connector.Config) (connector.Connector, error) {
		return &mockConnector{name: cfg.Name, healthy: true}, nil
	})

	if pc.Type() != "profiled" {
		t.Errorf("expected type profiled, got %s", pc.Type())
	}
}

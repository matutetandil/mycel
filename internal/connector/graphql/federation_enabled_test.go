package graphql

import (
	"context"
	"log/slog"
	"testing"
)

// `federation { enabled = false }` has to turn federation off.
//
// The attribute was parsed into the config and then never read: Connect
// enabled federation unconditionally, so a server told not to federate still
// published its entire schema through `_service { sdl }` — which is the one
// reason anybody writes that setting.
func TestFederationCanBeTurnedOff(t *testing.T) {
	for name, tc := range map[string]struct {
		federation *FederationServerConfig
		want       bool
	}{
		"no federation block at all": {federation: nil, want: true},
		"the block, enabled":         {federation: &FederationServerConfig{Enabled: true, Version: 2}, want: true},
		"the block, disabled":        {federation: &FederationServerConfig{Enabled: false}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			conn := NewServer("api", &ServerConfig{
				Port:       0,
				Endpoint:   "/graphql",
				Federation: tc.federation,
			}, slog.Default())

			if err := conn.Connect(context.Background()); err != nil {
				t.Fatalf("connect: %v", err)
			}
			defer func() { _ = conn.Close(context.Background()) }()

			if got := conn.schemaBuilder.IsFederationEnabled(); got != tc.want {
				t.Errorf("federation enabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// The version is still the block's to choose.
func TestFederationVersionComesFromTheBlock(t *testing.T) {
	conn := NewServer("api", &ServerConfig{
		Endpoint:   "/graphql",
		Federation: &FederationServerConfig{Enabled: true, Version: 1},
	}, slog.Default())

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if !conn.schemaBuilder.IsFederationEnabled() {
		t.Fatal("federation is off")
	}
}

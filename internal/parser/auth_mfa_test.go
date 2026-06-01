package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// parseAuthMFA is a small helper that writes an auth config to a temp file,
// parses it, and returns the resulting MFA config (or fails the test).
func parseAuthMFA(t *testing.T, hcl string) *struct {
	Enabled bool
	Methods []string
} {
	t.Helper()

	tmpFile := filepath.Join(t.TempDir(), "auth.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	config, err := NewHCLParser().ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if config.Auth == nil || config.Auth.MFA == nil {
		t.Fatalf("expected auth.mfa to be parsed, got auth=%v", config.Auth)
	}
	return &struct {
		Enabled bool
		Methods []string
	}{Enabled: config.Auth.MFA.Enabled, Methods: config.Auth.MFA.Methods}
}

// TestParseAuthMFAEnabledExplicit covers the regression where `enabled` was
// missing from the mfa BodySchema, so setting it was a hard parse error.
func TestParseAuthMFAEnabledExplicit(t *testing.T) {
	mfa := parseAuthMFA(t, `
auth {
  mfa {
    enabled = true
    methods = ["totp"]
  }
}
`)
	if !mfa.Enabled {
		t.Errorf("expected mfa.enabled = true, got false")
	}
}

// TestParseAuthMFAEnabledDefaultsOnPresence verifies that writing an mfa block
// opts MFA in even without an explicit `enabled` attribute.
func TestParseAuthMFAEnabledDefaultsOnPresence(t *testing.T) {
	mfa := parseAuthMFA(t, `
auth {
  mfa {
    methods = ["totp"]
  }
}
`)
	if !mfa.Enabled {
		t.Errorf("expected mfa.enabled to default to true when the mfa block is present, got false")
	}
}

// TestParseAuthMFAEnabledExplicitFalse verifies that an explicit `enabled = false`
// still disables MFA without having to remove the block.
func TestParseAuthMFAEnabledExplicitFalse(t *testing.T) {
	mfa := parseAuthMFA(t, `
auth {
  mfa {
    enabled = false
    methods = ["totp"]
  }
}
`)
	if mfa.Enabled {
		t.Errorf("expected explicit mfa.enabled = false to disable MFA, got true")
	}
}

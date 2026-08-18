package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/auth"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

func TestAHookNamingAFlowNobodyDeclaredIsRefused(t *testing.T) {
	// A hook runs at a moment nobody is watching, so a misspelled name would
	// otherwise surface as a log line during whatever it was meant to catch.
	config := &parser.Configuration{
		Flows: []*flow.Config{{Name: "record_sign_in"}},
		Auth: &auth.Config{
			Hooks: &auth.HooksConfig{
				AfterLogin:    &auth.HookConfig{Flow: "record_sign_in"},
				OnFailedLogin: &auth.HookConfig{Flow: "record_sing_in"},
			},
		},
	}

	errs := ValidateAuthHooks(config)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly the misspelt one", errs)
	}
	if !strings.Contains(errs[0].Error(), "record_sing_in") {
		t.Errorf("the error does not name the flow: %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "on_failed_login") {
		t.Errorf("the error does not name the hook: %v", errs[0])
	}
}

func TestAConfigurationWithNoHooksIsFine(t *testing.T) {
	if errs := ValidateAuthHooks(&parser.Configuration{}); len(errs) != 0 {
		t.Errorf("errors for a configuration with no auth: %v", errs)
	}
	if errs := ValidateAuthHooks(nil); len(errs) != 0 {
		t.Errorf("errors for no configuration at all: %v", errs)
	}
}

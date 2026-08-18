package parser

import "testing"

// env() always returns a string, so a boolean written as env("MFA_ON") arrives
// as one. It used to take the binary down; the point of the fix is that the
// value lands, not merely that nothing crashes.
func TestABooleanFromTheEnvironmentLands(t *testing.T) {
	for name, tc := range map[string]struct {
		written string
		want    bool
	}{
		"true":  {`"true"`, true},
		"yes":   {`"yes"`, true},
		"on":    {`"on"`, true},
		"one":   {`"1"`, true},
		"false": {`"false"`, false},
		"no":    {`"no"`, false},
		"off":   {`"off"`, false},
		"zero":  {`"0"`, false},
		"empty": {`""`, false},
		"a real boolean": {`true`, true},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := tryParse(t, `auth {
  jwt {
    secret = "a-secret-long-enough-for-signing"
  }
  mfa {
    enabled = `+tc.written+`
  }
}`)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if cfg.Auth == nil || cfg.Auth.MFA == nil {
				t.Fatal("no mfa block came back")
			}
			if cfg.Auth.MFA.Enabled != tc.want {
				t.Errorf("enabled = %v, want %v", cfg.Auth.MFA.Enabled, tc.want)
			}
		})
	}
}

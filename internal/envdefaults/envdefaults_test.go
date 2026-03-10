package envdefaults

import "testing"

func TestForEnvironment(t *testing.T) {
	tests := []struct {
		env              string
		wantLogLevel     string
		wantLogFormat    string
		wantHotReload    bool
		wantPlayground   bool
		wantRateLimit    bool
		wantCORS         bool
		wantVerboseErr   bool
		wantDetailHealth bool
	}{
		{"development", "debug", "text", true, true, false, true, true, true},
		{"dev", "debug", "text", true, true, false, true, true, true},
		{"", "debug", "text", true, true, false, true, true, true},
		{"unknown", "debug", "text", true, true, false, true, true, true},
		{"staging", "info", "json", true, true, true, false, true, true},
		{"stage", "info", "json", true, true, true, false, true, true},
		{"production", "warn", "json", false, false, true, false, false, false},
		{"prod", "warn", "json", false, false, true, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			d := ForEnvironment(tt.env)
			if d.LogLevel != tt.wantLogLevel {
				t.Errorf("LogLevel = %q, want %q", d.LogLevel, tt.wantLogLevel)
			}
			if d.LogFormat != tt.wantLogFormat {
				t.Errorf("LogFormat = %q, want %q", d.LogFormat, tt.wantLogFormat)
			}
			if d.HotReload != tt.wantHotReload {
				t.Errorf("HotReload = %v, want %v", d.HotReload, tt.wantHotReload)
			}
			if d.Playground != tt.wantPlayground {
				t.Errorf("Playground = %v, want %v", d.Playground, tt.wantPlayground)
			}
			if d.RateLimitEnabled != tt.wantRateLimit {
				t.Errorf("RateLimitEnabled = %v, want %v", d.RateLimitEnabled, tt.wantRateLimit)
			}
			if d.CORSPermissive != tt.wantCORS {
				t.Errorf("CORSPermissive = %v, want %v", d.CORSPermissive, tt.wantCORS)
			}
			if d.VerboseErrors != tt.wantVerboseErr {
				t.Errorf("VerboseErrors = %v, want %v", d.VerboseErrors, tt.wantVerboseErr)
			}
			if d.DetailedHealth != tt.wantDetailHealth {
				t.Errorf("DetailedHealth = %v, want %v", d.DetailedHealth, tt.wantDetailHealth)
			}
		})
	}
}

package flow

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"empty string returns zero", "", 0, false},
		{"whitespace trimmed", "  1h  ", time.Hour, false},
		{"stdlib seconds", "30s", 30 * time.Second, false},
		{"stdlib minutes", "5m", 5 * time.Minute, false},
		{"stdlib hours", "2h", 2 * time.Hour, false},
		{"stdlib compound", "1h30m", time.Hour + 30*time.Minute, false},

		// Day suffix — the canonical TTL baseline in the docs.
		{"single day", "1d", 24 * time.Hour, false},
		{"thirty days", "30d", 30 * 24 * time.Hour, false},
		{"zero days", "0d", 0, false},

		// Week suffix.
		{"single week", "1w", 7 * 24 * time.Hour, false},
		{"two weeks", "2w", 14 * 24 * time.Hour, false},

		// Malformed inputs must error explicitly — silent fallback to 0
		// (no expiry) would let "30days" or "thirty-d" pass review and
		// leak entries forever in Redis.
		{"bare d", "d", 0, true},
		{"non-numeric d", "abc d", 0, true},
		{"30days (typo)", "30days", 0, true},
		{"unknown unit", "5y", 0, true},
		{"negative days", "-1d", 0, true},
		{"negative weeks", "-2w", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDuration(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseDuration(%q) error = %v, wantErr=%v", tc.input, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseDuration_30dRegressionGuard locks the canonical "30d" baseline
// that appears in dedupe docs and Mercury's HCL configs. The pre-existing
// time.ParseDuration call returned an error on "30d" and the caller
// silently fell back to zero (= no expiry), leaving Redis to grow
// unbounded.
func TestParseDuration_30dRegressionGuard(t *testing.T) {
	got, err := ParseDuration("30d")
	if err != nil {
		t.Fatalf("30d must parse: %v", err)
	}
	want := 30 * 24 * time.Hour
	if got != want {
		t.Errorf("30d = %v, want %v (720h)", got, want)
	}
}

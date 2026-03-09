package plugin

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		major   int
		minor   int
		patch   int
		wantErr bool
	}{
		{"1.0.0", 1, 0, 0, false},
		{"v1.0.0", 1, 0, 0, false},
		{"2.3.4", 2, 3, 4, false},
		{"v0.1.0", 0, 1, 0, false},
		{"1.0", 1, 0, 0, false},
		{"1", 1, 0, 0, false},
		{"v10.20.30", 10, 20, 30, false},
		{"1.2.3-beta.1", 1, 2, 3, false},   // pre-release stripped
		{"1.2.3+build.123", 1, 2, 3, false}, // build metadata stripped
		{"", 0, 0, 0, true},
		{"abc", 0, 0, 0, true},
		{"v", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := ParseVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch {
				t.Errorf("ParseVersion(%q) = %d.%d.%d, want %d.%d.%d",
					tt.input, v.Major, v.Minor, v.Patch, tt.major, tt.minor, tt.patch)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.1", "1.0.0", 1},
		{"0.1.0", "0.0.1", 1},
		{"1.0.0", "1.0.1", -1},
	}

	for _, tt := range tests {
		a, _ := ParseVersion(tt.a)
		b, _ := ParseVersion(tt.b)
		got := a.Compare(b)
		if got != tt.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		input   string
		match   string // version that should match
		noMatch string // version that should NOT match
	}{
		// Empty/latest
		{"", "5.0.0", ""},
		{"latest", "5.0.0", ""},

		// Exact
		{"1.0.0", "1.0.0", "1.0.1"},
		{"v1.0.0", "1.0.0", "2.0.0"},

		// Operators
		{">= 1.0.0", "1.0.0", "0.9.0"},
		{">= 1.0.0", "2.0.0", "0.9.9"},
		{"> 1.0.0", "1.0.1", "1.0.0"},
		{"< 2.0.0", "1.9.9", "2.0.0"},
		{"<= 2.0.0", "2.0.0", "2.0.1"},
		{"!= 1.5.0", "1.4.0", "1.5.0"},

		// Caret
		{"^1.5.0", "1.5.0", "2.0.0"},
		{"^1.5.0", "1.9.9", "0.9.0"},
		{"^0.5.0", "0.5.1", "0.6.0"},

		// Tilde
		{"~1.5.0", "1.5.0", "1.6.0"},
		{"~1.5.0", "1.5.9", "1.6.0"},
		{"~> 1.5", "1.5.0", "1.6.0"},
		{"~> 2.0", "2.0.5", "2.1.0"},

		// Comma-separated
		{">= 1.0.0, < 2.0.0", "1.5.0", "2.0.0"},
		{">= 1.0, < 2.0", "1.0.0", "0.9.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cs, err := ParseConstraint(tt.input)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error: %v", tt.input, err)
			}

			if tt.match != "" {
				v, _ := ParseVersion(tt.match)
				if !cs.Match(v) {
					t.Errorf("constraint %q should match %s", tt.input, tt.match)
				}
			}

			if tt.noMatch != "" {
				v, _ := ParseVersion(tt.noMatch)
				if cs.Match(v) {
					t.Errorf("constraint %q should NOT match %s", tt.input, tt.noMatch)
				}
			}
		})
	}
}

func TestBestMatch(t *testing.T) {
	versions := []Version{
		{Major: 1, Minor: 0, Patch: 0},
		{Major: 1, Minor: 1, Patch: 0},
		{Major: 1, Minor: 2, Patch: 0},
		{Major: 2, Minor: 0, Patch: 0},
		{Major: 2, Minor: 1, Patch: 0},
	}

	tests := []struct {
		constraint string
		wantStr    string
		wantOk     bool
	}{
		{"^1.0.0", "v1.2.0", true},
		{"~1.1.0", "v1.1.0", true},
		{">= 2.0.0", "v2.1.0", true},
		{">= 3.0.0", "", false},
		{"", "v2.1.0", true}, // latest
		{"1.1.0", "v1.1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			cs, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, ok := BestMatch(versions, cs)
			if ok != tt.wantOk {
				t.Fatalf("BestMatch ok=%v, want %v", ok, tt.wantOk)
			}
			if ok && got.String() != tt.wantStr {
				t.Errorf("BestMatch = %s, want %s", got.String(), tt.wantStr)
			}
		})
	}
}

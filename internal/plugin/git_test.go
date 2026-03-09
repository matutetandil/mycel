package plugin

import (
	"testing"
)

func TestNormalizeGitURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Known hosts → SSH
		{"github.com/acme/mycel-sap", "git@github.com:acme/mycel-sap.git"},
		{"gitlab.com/org/plugin", "git@gitlab.com:org/plugin.git"},
		{"bitbucket.org/team/repo", "git@bitbucket.org:team/repo.git"},
		// Full URLs → as-is
		{"https://github.com/acme/repo.git", "https://github.com/acme/repo.git"},
		{"git://example.com/repo.git", "git://example.com/repo.git"},
		{"ssh://git@example.com/repo.git", "ssh://git@example.com/repo.git"},
		{"git@github.com:acme/repo.git", "git@github.com:acme/repo.git"},
		// Unknown hosts → HTTPS
		{"git.example.com/org/repo", "https://git.example.com/org/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeGitURL(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeGitURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeGitURLHTTPS(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"github.com/acme/mycel-sap", "https://github.com/acme/mycel-sap.git"},
		{"gitlab.com/org/plugin", "https://gitlab.com/org/plugin.git"},
		{"bitbucket.org/team/repo", "https://bitbucket.org/team/repo.git"},
		{"https://github.com/acme/repo.git", "https://github.com/acme/repo.git"},
		{"git@github.com:acme/repo.git", "git@github.com:acme/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeGitURLHTTPS(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeGitURLHTTPS(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsGitSource(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"github.com/acme/plugin", true},
		{"gitlab.com/acme/plugin", true},
		{"bitbucket.org/acme/plugin", true},
		{"https://example.com/repo.git", true},
		{"git.example.com/org/repo", true},
		{"./plugins/local", false},
		{"/abs/path/plugin", false},
		{"../relative/plugin", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsGitSource(tt.input)
			if got != tt.want {
				t.Errorf("IsGitSource(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGitAvailable(t *testing.T) {
	// git should be available in the test environment
	if err := GitAvailable(); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

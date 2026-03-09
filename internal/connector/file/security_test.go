package file

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolvePath_TraversalProtection(t *testing.T) {
	c := &Connector{
		config: &Config{
			BasePath: "/data/uploads",
		},
	}

	tests := []struct {
		name      string
		input     string
		mustBeIn  string // resolved path must start with this
	}{
		{
			name:     "simple relative path",
			input:    "report.pdf",
			mustBeIn: "/data/uploads",
		},
		{
			name:     "dot-dot traversal",
			input:    "../../etc/passwd",
			mustBeIn: "/data/uploads",
		},
		{
			name:     "deep traversal",
			input:    "../../../../../../../etc/shadow",
			mustBeIn: "/data/uploads",
		},
		{
			name:     "absolute path treated as relative",
			input:    "/etc/passwd",
			mustBeIn: "/data/uploads",
		},
		{
			name:     "mixed traversal and subdirectory",
			input:    "subdir/../../etc/passwd",
			mustBeIn: "/data/uploads",
		},
		{
			name:     "dot-only",
			input:    ".",
			mustBeIn: "/data/uploads",
		},
		{
			name:     "double dot only",
			input:    "..",
			mustBeIn: "/data/uploads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := c.resolvePath(tt.input)
			absResolved, _ := filepath.Abs(resolved)
			absBase, _ := filepath.Abs(tt.mustBeIn)

			if absResolved != absBase && !strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) {
				t.Errorf("path %q resolved to %q which escapes %q", tt.input, absResolved, absBase)
			}
		})
	}
}

func TestResolvePath_NoBasePath(t *testing.T) {
	c := &Connector{
		config: &Config{
			BasePath: "",
		},
	}

	// Without BasePath, paths are cleaned but not restricted
	resolved := c.resolvePath("some/file.txt")
	if resolved != "some/file.txt" {
		t.Errorf("expected 'some/file.txt', got %q", resolved)
	}
}

func TestResolvePath_AbsolutePathContained(t *testing.T) {
	c := &Connector{
		config: &Config{
			BasePath: "/data",
		},
	}

	// /etc/passwd should be treated as relative → /data/etc/passwd
	resolved := c.resolvePath("/etc/passwd")
	absResolved, _ := filepath.Abs(resolved)

	if !strings.HasPrefix(absResolved, "/data") {
		t.Errorf("absolute path should be contained in /data, got %q", absResolved)
	}
	if strings.Contains(absResolved, "/etc/passwd") && !strings.HasPrefix(absResolved, "/data") {
		t.Error("should not escape to real /etc/passwd")
	}
}

// newTestConnector creates a connector with a temp BasePath for testing.
func newTestConnector(basePath string) *Connector {
	return &Connector{
		name: "test-file",
		config: &Config{
			BasePath:    basePath,
			Format:      "json",
			Permissions: 0644,
		},
		handlers: make(map[string]HandlerFunc),
		known:    make(map[string]fileState),
	}
}

// Ensure fileState and HandlerFunc are importable for test compilation
var _ = fileState{modTime: time.Time{}, size: 0}

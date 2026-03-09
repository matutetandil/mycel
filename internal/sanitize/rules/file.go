package rules

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FilePathRule validates that path-like fields don't escape a base directory.
type FilePathRule struct {
	// BasePath is the directory that paths must stay within.
	BasePath string

	// PathFields are the field names that contain file paths.
	// Default: ["path", "source", "destination", "file"]
	PathFields []string
}

func (r *FilePathRule) Name() string { return "file_path_containment" }

func (r *FilePathRule) Sanitize(value interface{}) (interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok || r.BasePath == "" {
		return value, nil
	}

	fields := r.PathFields
	if len(fields) == 0 {
		fields = []string{"path", "source", "destination", "file"}
	}

	for _, field := range fields {
		if v, exists := m[field]; exists {
			if s, ok := v.(string); ok {
				if err := ValidatePathContainment(s, r.BasePath); err != nil {
					return nil, fmt.Errorf("field %q: %w", field, err)
				}
			}
		}
	}

	return value, nil
}

// ValidatePathContainment checks that a path resolves within the given base directory.
func ValidatePathContainment(path, basePath string) error {
	if path == "" {
		return nil
	}

	// Check for null bytes (can bypass checks in C-based systems)
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("path contains null byte")
	}

	// Resolve path relative to base
	cleaned := filepath.Clean("/" + path)
	resolved := filepath.Join(basePath, cleaned)

	absBase, _ := filepath.Abs(basePath)
	absResolved, _ := filepath.Abs(resolved)

	if absResolved != absBase && !strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes base directory %q", path, basePath)
	}

	return nil
}

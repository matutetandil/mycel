package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LockFile represents the plugins.lock file.
type LockFile struct {
	Version int                    `json:"version"`
	Plugins map[string]*LockEntry `json:"plugins"`
}

// LockEntry represents a locked plugin version.
type LockEntry struct {
	Source   string `json:"source"`
	Version string `json:"version"`
	Resolved string `json:"resolved"`
	LockedAt string `json:"locked_at"`
}

// NewLockFile creates a new empty lock file.
func NewLockFile() *LockFile {
	return &LockFile{
		Version: 1,
		Plugins: make(map[string]*LockEntry),
	}
}

// ReadLockFile reads a plugins.lock file from the config directory.
// Returns nil (no error) if the file doesn't exist.
func ReadLockFile(configDir string) (*LockFile, error) {
	path := lockFilePath(configDir)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read plugins.lock: %w", err)
	}

	var lf LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("failed to parse plugins.lock: %w", err)
	}

	if lf.Plugins == nil {
		lf.Plugins = make(map[string]*LockEntry)
	}

	return &lf, nil
}

// WriteLockFile writes the lock file atomically to the config directory.
func WriteLockFile(configDir string, lf *LockFile) error {
	path := lockFilePath(configDir)

	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plugins.lock: %w", err)
	}
	data = append(data, '\n')

	// Write to temp file first, then rename (atomic)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plugins.lock: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize plugins.lock: %w", err)
	}

	return nil
}

// GetEntry returns the lock entry for a plugin, or nil if not locked.
func (lf *LockFile) GetEntry(name string) *LockEntry {
	if lf == nil {
		return nil
	}
	return lf.Plugins[name]
}

// SetEntry adds or updates a lock entry.
func (lf *LockFile) SetEntry(name string, entry *LockEntry) {
	if lf.Plugins == nil {
		lf.Plugins = make(map[string]*LockEntry)
	}
	entry.LockedAt = time.Now().UTC().Format(time.RFC3339)
	lf.Plugins[name] = entry
}

// RemoveEntry removes a lock entry.
func (lf *LockFile) RemoveEntry(name string) {
	delete(lf.Plugins, name)
}

func lockFilePath(configDir string) string {
	return filepath.Join(configDir, "plugins.lock")
}

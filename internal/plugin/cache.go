package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CacheManager handles the local plugin cache directory (mycel_plugins/).
type CacheManager struct {
	// BaseDir is the project config directory (where mycel_plugins/ lives).
	BaseDir string
}

// NewCacheManager creates a cache manager for the given config directory.
func NewCacheManager(baseDir string) *CacheManager {
	return &CacheManager{BaseDir: baseDir}
}

// Dir returns the absolute path to the mycel_plugins/ directory.
func (c *CacheManager) Dir() string {
	return filepath.Join(c.BaseDir, "mycel_plugins")
}

// PluginDir returns the cache path for a specific plugin version.
// Format: mycel_plugins/github.com/org/repo@v1.0.0/
func (c *CacheManager) PluginDir(source string, version Version) string {
	dirName := source + "@" + version.String()
	return filepath.Join(c.Dir(), dirName)
}

// IsCached returns true if the plugin version is already cached.
func (c *CacheManager) IsCached(source string, version Version) bool {
	dir := c.PluginDir(source, version)
	manifest := filepath.Join(dir, "plugin.hcl")
	_, err := os.Stat(manifest)
	return err == nil
}

// EnsureDir creates the mycel_plugins/ directory if it doesn't exist.
func (c *CacheManager) EnsureDir() error {
	return os.MkdirAll(c.Dir(), 0755)
}

// CachedPlugin represents a plugin found in the cache.
type CachedPlugin struct {
	Name    string
	Source  string
	Version Version
	Path    string
}

// List returns all cached plugins.
func (c *CacheManager) List() ([]CachedPlugin, error) {
	cacheDir := c.Dir()
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil, nil
	}

	var plugins []CachedPlugin

	err := filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for plugin.hcl files
		if info.Name() != "plugin.hcl" {
			return nil
		}

		pluginDir := filepath.Dir(path)
		relPath, _ := filepath.Rel(cacheDir, pluginDir)

		// Parse source@version from directory name
		source, version := parsePluginDirName(relPath)
		if source == "" {
			return nil
		}

		v, err := ParseVersion(version)
		if err != nil {
			return nil
		}

		plugins = append(plugins, CachedPlugin{
			Source:  source,
			Version: v,
			Path:    pluginDir,
		})

		return filepath.SkipDir
	})

	return plugins, err
}

// Remove removes a specific cached plugin version.
func (c *CacheManager) Remove(source string, version Version) error {
	dir := c.PluginDir(source, version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %s@%s not found in cache", source, version.String())
	}
	return os.RemoveAll(dir)
}

// RemoveByName removes all cached versions of a plugin by searching for the source.
func (c *CacheManager) RemoveByName(source string) error {
	cacheDir := c.Dir()
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Walk into host directories (github.com/, gitlab.com/, etc.)
		hostDir := filepath.Join(cacheDir, entry.Name())
		if err := removeMatchingPlugins(hostDir, source); err == nil {
			removed++
		}
	}

	if removed == 0 {
		return fmt.Errorf("no cached versions found for %s", source)
	}
	return nil
}

// Clean removes the entire mycel_plugins/ directory.
func (c *CacheManager) Clean() error {
	return os.RemoveAll(c.Dir())
}

// CopyPlugin copies a local plugin directory into the cache.
// Used for plugins with copy = true.
func (c *CacheManager) CopyPlugin(source, destName string) (string, error) {
	if err := c.EnsureDir(); err != nil {
		return "", err
	}

	destDir := filepath.Join(c.Dir(), destName)

	// Remove existing copy
	os.RemoveAll(destDir)

	if err := copyDir(source, destDir); err != nil {
		return "", fmt.Errorf("failed to copy plugin from %s: %w", source, err)
	}

	return destDir, nil
}

// parsePluginDirName splits "github.com/org/repo@v1.0.0" into source and version.
func parsePluginDirName(name string) (source, version string) {
	idx := strings.LastIndex(name, "@")
	if idx < 0 {
		return name, ""
	}
	return name[:idx], name[idx+1:]
}

func removeMatchingPlugins(hostDir string, source string) error {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(hostDir, entry.Name())
		relPath, _ := filepath.Rel(filepath.Dir(hostDir), fullPath)
		src, _ := parsePluginDirName(relPath)
		if strings.Contains(src, source) || strings.HasSuffix(src, source) {
			os.RemoveAll(fullPath)
		}
	}
	return nil
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Skip .git directory
		if entry.Name() == ".git" {
			continue
		}

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}

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
	manifest := filepath.Join(dir, "plugin.mycel")
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

		// Look for plugin.mycel files
		if info.Name() != "plugin.mycel" {
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

// RemoveByName removes every cached version of a plugin.
//
// A cached plugin lives at <host>/<org>/<name>@<version>, so finding one means
// walking the tree rather than the directory below the cache: this looked one
// level down, which is the host, and compared the name against "github.com/acme".
// Nothing ever matched for a git-hosted plugin — and the count it returned was
// of host directories walked, not plugins removed, so `mycel plugin remove`
// reported success and deleted nothing. Every version stayed on disk for ever
// while whoever ran it believed it was gone.
//
// The name matches the whole source or its last element, so both of these work:
//
//	mycel plugin remove storefront
//	mycel plugin remove github.com/acme/storefront
//
// Matching on a substring would make "store" take "storefront" with it.
func (c *CacheManager) RemoveByName(source string) error {
	cacheDir := c.Dir()
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil
	}

	var toRemove []string
	err := filepath.WalkDir(cacheDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || path == cacheDir {
			return nil
		}
		if !strings.Contains(entry.Name(), "@") {
			return nil
		}

		relPath, relErr := filepath.Rel(cacheDir, path)
		if relErr != nil {
			return nil
		}
		cachedSource, _ := parsePluginDirName(filepath.ToSlash(relPath))
		if cachedSource == source || lastElement(cachedSource) == source {
			toRemove = append(toRemove, path)
		}
		// Nothing worth descending into: a version directory holds the plugin.
		return filepath.SkipDir
	})
	if err != nil {
		return err
	}

	if len(toRemove) == 0 {
		return fmt.Errorf("no cached versions found for %s", source)
	}

	for _, path := range toRemove {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

// lastElement is the plugin's own name, without the host and organisation it
// is published under.
func lastElement(source string) string {
	if idx := strings.LastIndex(source, "/"); idx >= 0 {
		return source[idx+1:]
	}
	return source
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

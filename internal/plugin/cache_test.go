package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheManager_Dir(t *testing.T) {
	c := NewCacheManager("/project")
	if c.Dir() != "/project/mycel_plugins" {
		t.Errorf("expected /project/mycel_plugins, got %s", c.Dir())
	}
}

func TestCacheManager_PluginDir(t *testing.T) {
	c := NewCacheManager("/project")
	v := Version{Major: 1, Minor: 2, Patch: 3}
	got := c.PluginDir("github.com/acme/plugin", v)
	want := "/project/mycel_plugins/github.com/acme/plugin@v1.2.3"
	if got != want {
		t.Errorf("PluginDir = %q, want %q", got, want)
	}
}

func TestCacheManager_IsCached(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)
	v := Version{Major: 1, Minor: 0, Patch: 0}

	// Not cached initially
	if c.IsCached("github.com/acme/plugin", v) {
		t.Error("expected not cached")
	}

	// Create cache entry with plugin.hcl
	dir := c.PluginDir("github.com/acme/plugin", v)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "plugin.mycel"), []byte("plugin {}"), 0644)

	// Now cached
	if !c.IsCached("github.com/acme/plugin", v) {
		t.Error("expected cached after creating plugin.mycel")
	}
}

func TestCacheManager_EnsureDir(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)

	if err := c.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	info, err := os.Stat(c.Dir())
	if err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestCacheManager_List_Empty(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)

	// No mycel_plugins/ dir → returns nil
	plugins, err := c.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if plugins != nil {
		t.Errorf("expected nil, got %v", plugins)
	}
}

func TestCacheManager_List_WithPlugins(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)

	// Create a cached plugin
	v := Version{Major: 2, Minor: 1, Patch: 0}
	dir := c.PluginDir("github.com/acme/sap", v)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "plugin.mycel"), []byte(`plugin { name = "sap" version = "2.1.0" }`), 0644)

	plugins, err := c.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Version.Major != 2 || plugins[0].Version.Minor != 1 {
		t.Errorf("unexpected version: %s", plugins[0].Version.String())
	}
}

func TestCacheManager_Remove(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)
	v := Version{Major: 1, Minor: 0, Patch: 0}

	// Create cached entry
	dir := c.PluginDir("github.com/acme/plugin", v)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "plugin.mycel"), []byte("plugin {}"), 0644)

	// Remove it
	if err := c.Remove("github.com/acme/plugin", v); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify it's gone
	if c.IsCached("github.com/acme/plugin", v) {
		t.Error("expected not cached after remove")
	}
}

func TestCacheManager_Remove_NotFound(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)
	v := Version{Major: 9, Minor: 9, Patch: 9}

	err := c.Remove("nonexistent", v)
	if err == nil {
		t.Error("expected error removing non-existent plugin")
	}
}

func TestCacheManager_CopyPlugin(t *testing.T) {
	tmp := t.TempDir()

	// Create a "local plugin" directory
	localPlugin := filepath.Join(tmp, "my-local-plugin")
	os.MkdirAll(localPlugin, 0755)
	os.WriteFile(filepath.Join(localPlugin, "plugin.mycel"), []byte("plugin {}"), 0644)
	os.WriteFile(filepath.Join(localPlugin, "connector.wasm"), []byte("fake-wasm"), 0644)

	// Create a .git dir that should be skipped
	os.MkdirAll(filepath.Join(localPlugin, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(localPlugin, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)

	c := NewCacheManager(tmp)
	dest, err := c.CopyPlugin(localPlugin, "my-local-plugin")
	if err != nil {
		t.Fatalf("CopyPlugin failed: %v", err)
	}

	// Verify files were copied
	if _, err := os.Stat(filepath.Join(dest, "plugin.mycel")); err != nil {
		t.Error("plugin.hcl not copied")
	}
	if _, err := os.Stat(filepath.Join(dest, "connector.wasm")); err != nil {
		t.Error("connector.wasm not copied")
	}

	// Verify .git was NOT copied
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		t.Error(".git directory should not be copied")
	}
}

func TestCacheManager_Clean(t *testing.T) {
	tmp := t.TempDir()
	c := NewCacheManager(tmp)

	// Create cache with content
	c.EnsureDir()
	os.WriteFile(filepath.Join(c.Dir(), "test"), []byte("x"), 0644)

	if err := c.Clean(); err != nil {
		t.Fatalf("Clean failed: %v", err)
	}

	if _, err := os.Stat(c.Dir()); !os.IsNotExist(err) {
		t.Error("expected cache dir removed after Clean")
	}
}

func TestParsePluginDirName(t *testing.T) {
	tests := []struct {
		input       string
		wantSource  string
		wantVersion string
	}{
		{"github.com/acme/plugin@v1.0.0", "github.com/acme/plugin", "v1.0.0"},
		{"my-plugin", "my-plugin", ""},
		{"gitlab.com/org/repo@v2.3.4", "gitlab.com/org/repo", "v2.3.4"},
	}

	for _, tt := range tests {
		source, version := parsePluginDirName(tt.input)
		if source != tt.wantSource || version != tt.wantVersion {
			t.Errorf("parsePluginDirName(%q) = (%q, %q), want (%q, %q)",
				tt.input, source, version, tt.wantSource, tt.wantVersion)
		}
	}
}

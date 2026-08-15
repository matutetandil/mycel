package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// The cache a plugin is installed into, and taking one out of it. `mycel plugin
// remove` is the command behind this, and removing the wrong thing is worse
// than removing nothing: the plugin a service still declares disappears from
// under it and the next start fails on a module that was there yesterday.

// cached writes a plugin into the cache the way a clone leaves it.
func cached(t *testing.T, cache *CacheManager, source string, version Version) string {
	t.Helper()
	dir := cache.PluginDir(source, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.mycel"), []byte(`
plugin {
  name    = "cached"
  version = "1.0.0"
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

func TestAVersionInTheCacheIsNotClonedAgain(t *testing.T) {
	cache := NewCacheManager(t.TempDir())
	version, err := ParseVersion("v1.0.0")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}

	if cache.IsCached("github.com/acme/plugin", version) {
		t.Error("a plugin nobody installed is reported as cached")
	}

	cached(t, cache, "github.com/acme/plugin", version)

	if !cache.IsCached("github.com/acme/plugin", version) {
		t.Error("an installed plugin is not reported as cached")
	}
	// Another version of the same plugin is a different entry, which is what
	// lets two services on one machine pin different versions.
	other, err := ParseVersion("v2.0.0")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	if cache.IsCached("github.com/acme/plugin", other) {
		t.Error("one version being installed made another look installed")
	}
}

func TestADirectoryWithNoManifestIsNotAPlugin(t *testing.T) {
	// A clone that died halfway leaves a directory behind. Treating it as
	// installed means the next start loads nothing and says the manifest is
	// missing, rather than fetching it again.
	cache := NewCacheManager(t.TempDir())
	version, _ := ParseVersion("v1.0.0")

	if err := os.MkdirAll(cache.PluginDir("github.com/acme/plugin", version), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if cache.IsCached("github.com/acme/plugin", version) {
		t.Error("a half-finished clone is reported as installed")
	}
}

func TestRemovingOneVersionLeavesTheOthers(t *testing.T) {
	cache := NewCacheManager(t.TempDir())
	one, _ := ParseVersion("v1.0.0")
	two, _ := ParseVersion("v2.0.0")

	cached(t, cache, "github.com/acme/plugin", one)
	cached(t, cache, "github.com/acme/plugin", two)

	if err := cache.Remove("github.com/acme/plugin", one); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if cache.IsCached("github.com/acme/plugin", one) {
		t.Error("the version that was removed is still there")
	}
	if !cache.IsCached("github.com/acme/plugin", two) {
		t.Error("removing one version took another with it")
	}

	// Removing what is not there says so rather than reporting success, or
	// `mycel plugin remove` on a typo looks like it worked.
	if err := cache.Remove("github.com/acme/plugin", one); err == nil {
		t.Error("removing a version that is not installed reported success")
	}
}

func TestRemovingAPluginByNameTakesEveryVersionOfIt(t *testing.T) {
	// What `mycel plugin remove <name>` does, and the thing to get right is
	// which plugins match: another plugin left behind is a disk that fills up,
	// another plugin removed is a service that stops starting.
	cache := NewCacheManager(t.TempDir())
	one, _ := ParseVersion("v1.0.0")
	two, _ := ParseVersion("v2.0.0")

	cached(t, cache, "github.com/acme/storefront", one)
	cached(t, cache, "github.com/acme/storefront", two)
	cached(t, cache, "github.com/acme/warehouse", one)

	if err := cache.RemoveByName("storefront"); err != nil {
		t.Fatalf("RemoveByName: %v", err)
	}

	if cache.IsCached("github.com/acme/storefront", one) || cache.IsCached("github.com/acme/storefront", two) {
		t.Error("a version of the plugin that was removed is still installed")
	}
	if !cache.IsCached("github.com/acme/warehouse", one) {
		t.Error("removing one plugin took another with it")
	}
}

func TestRemovingFromACacheThatIsNotThereIsHarmless(t *testing.T) {
	// `mycel plugin remove` on a project that never installed anything.
	cache := NewCacheManager(t.TempDir())

	if err := cache.RemoveByName("storefront"); err != nil {
		t.Errorf("removing from an empty cache failed: %v", err)
	}
}

func TestALocalPluginCanBeCopiedIntoTheCache(t *testing.T) {
	// copy = true, for a plugin developed beside the service: the copy is what
	// runs, so what is built is what ships rather than whatever the working
	// directory holds at the time.
	base := t.TempDir()
	cache := NewCacheManager(base)

	source := filepath.Join(base, "in-development")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for path, body := range map[string]string{
		"plugin.mycel":     "plugin {\n  name = \"local\"\n}\n",
		"connector.wasm":   "not really a module",
		"nested/extra.txt": "something in a subdirectory",
	} {
		if err := os.WriteFile(filepath.Join(source, path), []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	copied, err := cache.CopyPlugin(source, "local")
	if err != nil {
		t.Fatalf("CopyPlugin: %v", err)
	}

	for _, path := range []string{"plugin.mycel", "connector.wasm", "nested/extra.txt"} {
		if _, err := os.Stat(filepath.Join(copied, path)); err != nil {
			t.Errorf("%s did not survive the copy: %v", path, err)
		}
	}

	// Copying again replaces what was there, which is what rebuilding a
	// plugin during development means.
	if err := os.WriteFile(filepath.Join(source, "connector.wasm"), []byte("rebuilt"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := cache.CopyPlugin(source, "local"); err != nil {
		t.Fatalf("CopyPlugin: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(copied, "connector.wasm"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "rebuilt" {
		t.Errorf("the copy still holds the previous build: %q", body)
	}
}

func TestRemovingByNameDoesNotTakeAPluginWithASimilarName(t *testing.T) {
	// Matching on a substring would make removing "store" take "storefront"
	// with it — and the service that declared storefront stops starting on a
	// module that was there yesterday.
	cache := NewCacheManager(t.TempDir())
	version, _ := ParseVersion("v1.0.0")

	cached(t, cache, "github.com/acme/store", version)
	cached(t, cache, "github.com/acme/storefront", version)

	if err := cache.RemoveByName("store"); err != nil {
		t.Fatalf("RemoveByName: %v", err)
	}

	if cache.IsCached("github.com/acme/store", version) {
		t.Error("the plugin that was named is still installed")
	}
	if !cache.IsCached("github.com/acme/storefront", version) {
		t.Error("a plugin whose name merely starts the same was removed")
	}
}

func TestAPluginCanBeRemovedByItsWholeSource(t *testing.T) {
	// Which is what the lock file records, and what `mycel plugin remove`
	// looks up when the short name is ambiguous.
	cache := NewCacheManager(t.TempDir())
	version, _ := ParseVersion("v1.0.0")

	cached(t, cache, "github.com/acme/storefront", version)

	if err := cache.RemoveByName("github.com/acme/storefront"); err != nil {
		t.Fatalf("RemoveByName: %v", err)
	}
	if cache.IsCached("github.com/acme/storefront", version) {
		t.Error("the plugin is still installed")
	}
}

func TestRemovingAPluginNobodyInstalledSaysSo(t *testing.T) {
	// Rather than reporting success, which is what made `mycel plugin remove`
	// look like it worked while every version stayed on disk.
	cache := NewCacheManager(t.TempDir())
	version, _ := ParseVersion("v1.0.0")
	cached(t, cache, "github.com/acme/storefront", version)

	if err := cache.RemoveByName("a-plugin-nobody-installed"); err == nil {
		t.Error("removing a plugin that is not installed reported success")
	}
}

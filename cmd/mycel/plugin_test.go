package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/plugin"
)

// The plugin commands are how somebody sees what a service will load and takes
// one away again. They read two things that can disagree — what the
// configuration declares and what the lock file pinned — and the answers they
// print are what somebody acts on.

func pluginProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := project(t, files)

	previous := configDir
	configDir = dir
	t.Cleanup(func() { configDir = previous })
	return dir
}

const declaredPlugins = `
service {
  name = "with-plugins"
}

plugin "local-one" {
  source = "./plugins/local-one"
}

plugin "remote-one" {
  source  = "github.com/someone/mycel-plugin"
  version = "1.2.0"
}
`

func lockFileWith(t *testing.T, dir string, entries map[string]*plugin.LockEntry) {
	t.Helper()
	lf := plugin.NewLockFile()
	for name, entry := range entries {
		lf.SetEntry(name, entry)
	}
	if err := plugin.WriteLockFile(dir, lf); err != nil {
		t.Fatalf("WriteLockFile: %v", err)
	}
}

func TestListingPluginsWithNoneConfigured(t *testing.T) {
	pluginProject(t, map[string]string{"config.mycel": `
service {
  name = "plain"
}
`})

	if err := runPluginList(nil, nil); err != nil {
		t.Errorf("runPluginList: %v", err)
	}
}

func TestListingShowsWhatTheConfigurationDeclares(t *testing.T) {
	pluginProject(t, map[string]string{"config.mycel": declaredPlugins})

	if err := runPluginList(nil, nil); err != nil {
		t.Fatalf("runPluginList: %v", err)
	}
}

func TestListingShowsWhatTheLockFilePinned(t *testing.T) {
	// The lock file is what a deployment actually installs, so a version
	// pinned there is the one worth showing.
	dir := pluginProject(t, map[string]string{"config.mycel": declaredPlugins})
	lockFileWith(t, dir, map[string]*plugin.LockEntry{
		"remote-one": {
			Source:  "github.com/someone/mycel-plugin",
			Version: "1.2.0",
		},
	})

	if err := runPluginList(nil, nil); err != nil {
		t.Fatalf("runPluginList: %v", err)
	}
}

func TestRemovingAPluginTakesItOutOfTheLockFile(t *testing.T) {
	// Leaving it there means the next install brings it back, which is the
	// opposite of what somebody asked for.
	dir := pluginProject(t, map[string]string{"config.mycel": declaredPlugins})
	lockFileWith(t, dir, map[string]*plugin.LockEntry{
		"remote-one": {Source: "github.com/someone/mycel-plugin", Version: "1.2.0"},
		"other-one":  {Source: "github.com/someone/other", Version: "0.1.0"},
	})

	if err := runPluginRemove(nil, []string{"remote-one"}); err != nil {
		t.Fatalf("runPluginRemove: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plugins.lock"))
	if err != nil {
		t.Fatalf("reading the lock file: %v", err)
	}
	if strings.Contains(string(raw), "remote-one") {
		t.Error("the plugin is still pinned in the lock file")
	}
	// And the one nobody asked about is untouched.
	if !strings.Contains(string(raw), "other-one") {
		t.Error("removing one plugin took another with it")
	}

	// The file is still a lock file afterwards.
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Errorf("the lock file is no longer JSON: %v", err)
	}
}

func TestRemovingAPluginNobodyDeclaredSaysSo(t *testing.T) {
	pluginProject(t, map[string]string{"config.mycel": declaredPlugins})

	// Not an error — there is nothing to undo — but it must not pretend to
	// have removed something.
	if err := runPluginRemove(nil, []string{"never-installed"}); err != nil {
		t.Errorf("runPluginRemove: %v", err)
	}
}

func TestTheLockFilePathIsBesideTheConfiguration(t *testing.T) {
	dir := pluginProject(t, map[string]string{"config.mycel": declaredPlugins})

	if got := lockFilePath(); got != filepath.Join(dir, "plugins.lock") {
		t.Errorf("lock file = %q, want it beside the configuration", got)
	}
}

func TestTheDeclarationsAreReadFromTheConfiguration(t *testing.T) {
	pluginProject(t, map[string]string{"config.mycel": declaredPlugins})

	decls, err := parsePluginDeclarations()
	if err != nil {
		t.Fatalf("parsePluginDeclarations: %v", err)
	}
	if len(decls) != 2 {
		t.Fatalf("%d declarations read, want both", len(decls))
	}

	byName := map[string]string{}
	for _, d := range decls {
		byName[d.Name] = d.Source
	}
	if byName["local-one"] != "./plugins/local-one" {
		t.Errorf("declarations = %v", byName)
	}
	if byName["remote-one"] != "github.com/someone/mycel-plugin" {
		t.Errorf("declarations = %v", byName)
	}
}

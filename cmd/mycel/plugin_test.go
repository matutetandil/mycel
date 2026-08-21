package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/plugin"
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

// An update that fails must not take the lock file with it.
//
// The lock file is the only record of what this deployment is pinned to, and
// resolving afresh means removing it. But an update that fails writes no new
// one — LoadAll stops at the first plugin it cannot load and saves nothing —
// so removing it up front left no lock file at all, and the next start floated
// every plugin to whatever resolved that day.

func TestAFailedUpdateLeavesThePinsAlone(t *testing.T) {
	dir := pluginProject(t, map[string]string{
		"service.mycel": `service {
  name = "with-plugins"
}

plugin "nowhere" {
  source = "./plugins/nowhere"
}`,
	})

	lockPath := filepath.Join(dir, "plugins.lock")
	pinned := `{"version":1,"plugins":{"nowhere":{"version":"1.0.0","resolved":"./plugins/nowhere"}}}`
	if err := os.WriteFile(lockPath, []byte(pinned), 0o644); err != nil {
		t.Fatal(err)
	}

	// The plugin's source does not exist, so the update cannot succeed.
	err := runPluginUpdate(nil, nil)
	if err == nil {
		t.Fatal("updating a plugin that cannot be loaded reported success")
	}

	after, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatalf("the lock file is gone after a failed update, so the next start "+
			"resolves every plugin afresh: %v", readErr)
	}
	if string(after) != pinned {
		t.Errorf("the lock file changed after a failed update:\n got %s\nwant %s", after, pinned)
	}
}

func TestAFailedUpdateWithNoLockFileLeavesNone(t *testing.T) {
	// Nothing to put back, and nothing to invent.
	dir := pluginProject(t, map[string]string{
		"service.mycel": `service {
  name = "with-plugins"
}

plugin "nowhere" {
  source = "./plugins/nowhere"
}`,
	})

	if err := runPluginUpdate(nil, nil); err == nil {
		t.Fatal("updating a plugin that cannot be loaded reported success")
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins.lock")); !os.IsNotExist(err) {
		t.Errorf("a lock file appeared out of a failed update: %v", err)
	}
}

func TestUpdatingByNameRefusesAPluginNobodyDeclared(t *testing.T) {
	pluginProject(t, map[string]string{"service.mycel": declaredPlugins})

	err := runPluginUpdate(nil, []string{"not-declared"})
	if err == nil {
		t.Fatal("a plugin the configuration does not declare was updated")
	}
	if !strings.Contains(err.Error(), "not-declared") {
		t.Errorf("the error does not name it: %v", err)
	}
}

func TestUpdatingAConfigurationWithNoPluginsSaysSo(t *testing.T) {
	pluginProject(t, map[string]string{"service.mycel": `service {
  name = "plain"
}`})

	if err := runPluginUpdate(nil, nil); err != nil {
		t.Errorf("a service with no plugins errored: %v", err)
	}
}

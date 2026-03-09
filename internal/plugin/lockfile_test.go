package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewLockFile(t *testing.T) {
	lf := NewLockFile()
	if lf.Version != 1 {
		t.Errorf("expected version 1, got %d", lf.Version)
	}
	if lf.Plugins == nil {
		t.Error("expected Plugins map initialized")
	}
}

func TestLockFile_SetGetRemove(t *testing.T) {
	lf := NewLockFile()

	// Set
	lf.SetEntry("salesforce", &LockEntry{
		Source:   "github.com/acme/salesforce",
		Version:  "v1.2.0",
		Resolved: "git@github.com:acme/salesforce.git",
	})

	// Get
	entry := lf.GetEntry("salesforce")
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Version != "v1.2.0" {
		t.Errorf("expected version v1.2.0, got %s", entry.Version)
	}
	if entry.LockedAt == "" {
		t.Error("expected LockedAt to be set")
	}

	// Get non-existent
	if lf.GetEntry("nonexistent") != nil {
		t.Error("expected nil for non-existent entry")
	}

	// Remove
	lf.RemoveEntry("salesforce")
	if lf.GetEntry("salesforce") != nil {
		t.Error("expected nil after removal")
	}
}

func TestLockFile_GetEntry_Nil(t *testing.T) {
	var lf *LockFile
	if lf.GetEntry("anything") != nil {
		t.Error("expected nil from nil lockfile")
	}
}

func TestWriteAndReadLockFile(t *testing.T) {
	tmp := t.TempDir()

	// Write
	lf := NewLockFile()
	lf.SetEntry("sap", &LockEntry{
		Source:   "github.com/acme/sap",
		Version:  "v2.0.0",
		Resolved: "git@github.com:acme/sap.git",
	})
	lf.SetEntry("stripe", &LockEntry{
		Source:   "github.com/acme/stripe",
		Version:  "v1.5.3",
		Resolved: "git@github.com:acme/stripe.git",
	})

	if err := WriteLockFile(tmp, lf); err != nil {
		t.Fatalf("WriteLockFile failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmp, "plugins.lock")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plugins.lock not created: %v", err)
	}

	// Read back
	read, err := ReadLockFile(tmp)
	if err != nil {
		t.Fatalf("ReadLockFile failed: %v", err)
	}
	if read == nil {
		t.Fatal("expected lockfile, got nil")
	}
	if len(read.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(read.Plugins))
	}
	if read.GetEntry("sap").Version != "v2.0.0" {
		t.Errorf("sap version mismatch: %s", read.GetEntry("sap").Version)
	}
	if read.GetEntry("stripe").Version != "v1.5.3" {
		t.Errorf("stripe version mismatch: %s", read.GetEntry("stripe").Version)
	}
}

func TestReadLockFile_NotExists(t *testing.T) {
	tmp := t.TempDir()

	lf, err := ReadLockFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lf != nil {
		t.Error("expected nil for non-existent lockfile")
	}
}

func TestReadLockFile_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "plugins.lock"), []byte("not json"), 0644)

	_, err := ReadLockFile(tmp)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWriteLockFile_Atomic(t *testing.T) {
	tmp := t.TempDir()

	lf := NewLockFile()
	lf.SetEntry("test", &LockEntry{
		Source:  "github.com/test/plugin",
		Version: "v1.0.0",
	})

	if err := WriteLockFile(tmp, lf); err != nil {
		t.Fatalf("WriteLockFile failed: %v", err)
	}

	// Verify no .tmp file left behind
	tmpFile := filepath.Join(tmp, "plugins.lock.tmp")
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}

	// Verify the file is valid JSON
	data, _ := os.ReadFile(filepath.Join(tmp, "plugins.lock"))
	var check LockFile
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
}

func TestWriteLockFile_Overwrite(t *testing.T) {
	tmp := t.TempDir()

	// Write v1
	lf := NewLockFile()
	lf.SetEntry("plugin", &LockEntry{Version: "v1.0.0"})
	WriteLockFile(tmp, lf)

	// Write v2
	lf.SetEntry("plugin", &LockEntry{Version: "v2.0.0"})
	WriteLockFile(tmp, lf)

	// Read should get v2
	read, _ := ReadLockFile(tmp)
	if read.GetEntry("plugin").Version != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %s", read.GetEntry("plugin").Version)
	}
}

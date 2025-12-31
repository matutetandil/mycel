package hotreload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_Basic(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "mycel-hotreload-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial config file
	configFile := filepath.Join(tmpDir, "config.hcl")
	if err := os.WriteFile(configFile, []byte("# initial"), 0644); err != nil {
		t.Fatal(err)
	}

	reloadCount := atomic.Int32{}

	watcher, err := NewWatcher(
		&Config{
			Enabled:    true,
			Paths:      []string{tmpDir},
			Extensions: []string{".hcl"},
			Debounce:   50 * time.Millisecond,
		},
		nil,
		func(ctx context.Context) error {
			reloadCount.Add(1)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Modify the file
	if err := os.WriteFile(configFile, []byte("# modified"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce and reload
	time.Sleep(200 * time.Millisecond)

	if reloadCount.Load() == 0 {
		t.Error("expected at least one reload")
	}
}

func TestWatcher_Debounce(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-hotreload-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.hcl")
	if err := os.WriteFile(configFile, []byte("# initial"), 0644); err != nil {
		t.Fatal(err)
	}

	reloadCount := atomic.Int32{}

	watcher, err := NewWatcher(
		&Config{
			Enabled:    true,
			Paths:      []string{tmpDir},
			Extensions: []string{".hcl"},
			Debounce:   100 * time.Millisecond,
		},
		nil,
		func(ctx context.Context) error {
			reloadCount.Add(1)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	time.Sleep(50 * time.Millisecond)

	// Rapid modifications
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(configFile, []byte("# modified "+string(rune('0'+i))), 0644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(200 * time.Millisecond)

	// Should only reload once due to debouncing
	if reloadCount.Load() != 1 {
		t.Errorf("expected 1 reload (debounced), got %d", reloadCount.Load())
	}
}

func TestWatcher_ExtensionFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-hotreload-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create files with different extensions
	hclFile := filepath.Join(tmpDir, "config.hcl")
	txtFile := filepath.Join(tmpDir, "readme.txt")
	if err := os.WriteFile(hclFile, []byte("# initial"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txtFile, []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}

	reloadCount := atomic.Int32{}

	watcher, err := NewWatcher(
		&Config{
			Enabled:    true,
			Paths:      []string{tmpDir},
			Extensions: []string{".hcl"},
			Debounce:   50 * time.Millisecond,
		},
		nil,
		func(ctx context.Context) error {
			reloadCount.Add(1)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	time.Sleep(50 * time.Millisecond)

	// Modify txt file (should not trigger reload)
	if err := os.WriteFile(txtFile, []byte("modified readme"), 0644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	if reloadCount.Load() != 0 {
		t.Error("expected no reload for .txt file")
	}

	// Modify hcl file (should trigger reload)
	if err := os.WriteFile(hclFile, []byte("# modified"), 0644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	if reloadCount.Load() == 0 {
		t.Error("expected reload for .hcl file")
	}
}

func TestWatcher_ValidationFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-hotreload-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.hcl")
	if err := os.WriteFile(configFile, []byte("# initial"), 0644); err != nil {
		t.Fatal(err)
	}

	validateCalled := atomic.Bool{}
	reloadCalled := atomic.Bool{}
	validationError := errors.New("validation failed")

	watcher, err := NewWatcher(
		&Config{
			Enabled:    true,
			Paths:      []string{tmpDir},
			Extensions: []string{".hcl"},
			Debounce:   50 * time.Millisecond,
		},
		nil,
		func(ctx context.Context) error {
			reloadCalled.Store(true)
			return nil
		},
		func(ctx context.Context) error {
			validateCalled.Store(true)
			return validationError
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	time.Sleep(50 * time.Millisecond)

	// Trigger reload
	if err := os.WriteFile(configFile, []byte("# modified"), 0644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	if !validateCalled.Load() {
		t.Error("expected validate to be called")
	}

	if reloadCalled.Load() {
		t.Error("reload should not be called when validation fails")
	}
}

func TestWatcher_Disabled(t *testing.T) {
	watcher, err := NewWatcher(
		&Config{
			Enabled: false,
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := watcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	// Should not error when disabled
}

func TestWatcher_TriggerReload(t *testing.T) {
	reloadCalled := atomic.Bool{}

	watcher, err := NewWatcher(
		&Config{
			Enabled: true,
		},
		nil,
		func(ctx context.Context) error {
			reloadCalled.Store(true)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := watcher.TriggerReload(ctx); err != nil {
		t.Fatal(err)
	}

	if !reloadCalled.Load() {
		t.Error("expected reload to be called")
	}
}

func TestReloader_Basic(t *testing.T) {
	loadCalled := atomic.Bool{}
	validateCalled := atomic.Bool{}
	prepareCalled := atomic.Bool{}
	switchCalled := atomic.Bool{}
	completeCalled := atomic.Bool{}

	reloader := NewReloader(&ReloaderConfig{
		ConfigPath: "/test/config",
		OnLoad: func(ctx context.Context, path string) error {
			loadCalled.Store(true)
			return nil
		},
		OnValidate: func(ctx context.Context) error {
			validateCalled.Store(true)
			return nil
		},
		OnPrepare: func(ctx context.Context) error {
			prepareCalled.Store(true)
			return nil
		},
		OnSwitch: func(ctx context.Context) error {
			switchCalled.Store(true)
			return nil
		},
		OnComplete: func(ctx context.Context) {
			completeCalled.Store(true)
		},
	})

	ctx := context.Background()
	if err := reloader.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	if !loadCalled.Load() {
		t.Error("expected OnLoad to be called")
	}
	if !validateCalled.Load() {
		t.Error("expected OnValidate to be called")
	}
	if !prepareCalled.Load() {
		t.Error("expected OnPrepare to be called")
	}
	if !switchCalled.Load() {
		t.Error("expected OnSwitch to be called")
	}
	if !completeCalled.Load() {
		t.Error("expected OnComplete to be called")
	}

	if reloader.Version() != 1 {
		t.Errorf("expected version 1, got %d", reloader.Version())
	}

	if reloader.LastSuccess().IsZero() {
		t.Error("expected last success to be set")
	}

	if reloader.LastError() != nil {
		t.Errorf("expected no error, got %v", reloader.LastError())
	}
}

func TestReloader_LoadFailure(t *testing.T) {
	loadError := errors.New("load failed")

	reloader := NewReloader(&ReloaderConfig{
		ConfigPath: "/test/config",
		OnLoad: func(ctx context.Context, path string) error {
			return loadError
		},
		OnValidate: func(ctx context.Context) error {
			t.Error("OnValidate should not be called on load failure")
			return nil
		},
	})

	ctx := context.Background()
	err := reloader.Reload(ctx)

	if err == nil {
		t.Error("expected error")
	}

	if reloader.Version() != 0 {
		t.Errorf("expected version 0, got %d", reloader.Version())
	}

	if reloader.LastError() == nil {
		t.Error("expected last error to be set")
	}
}

func TestReloader_SwitchFailureRollback(t *testing.T) {
	prepareCalled := atomic.Bool{}
	switchError := errors.New("switch failed")
	rollbackCalled := atomic.Bool{}

	reloader := NewReloader(&ReloaderConfig{
		ConfigPath: "/test/config",
		OnPrepare: func(ctx context.Context) error {
			prepareCalled.Store(true)
			return nil
		},
		OnSwitch: func(ctx context.Context) error {
			return switchError
		},
		OnRollback: func(ctx context.Context, err error) {
			rollbackCalled.Store(true)
			if !errors.Is(err, switchError) {
				t.Errorf("expected switch error, got %v", err)
			}
		},
	})

	ctx := context.Background()
	err := reloader.Reload(ctx)

	if err == nil {
		t.Error("expected error")
	}

	if !prepareCalled.Load() {
		t.Error("expected OnPrepare to be called")
	}

	if !rollbackCalled.Load() {
		t.Error("expected OnRollback to be called")
	}
}

func TestReloader_Validate(t *testing.T) {
	loadCalled := atomic.Bool{}
	validateCalled := atomic.Bool{}
	prepareCalled := atomic.Bool{}

	reloader := NewReloader(&ReloaderConfig{
		ConfigPath: "/test/config",
		OnLoad: func(ctx context.Context, path string) error {
			loadCalled.Store(true)
			return nil
		},
		OnValidate: func(ctx context.Context) error {
			validateCalled.Store(true)
			return nil
		},
		OnPrepare: func(ctx context.Context) error {
			prepareCalled.Store(true)
			return nil
		},
	})

	ctx := context.Background()
	if err := reloader.Validate(ctx); err != nil {
		t.Fatal(err)
	}

	if !loadCalled.Load() {
		t.Error("expected OnLoad to be called")
	}
	if !validateCalled.Load() {
		t.Error("expected OnValidate to be called")
	}
	if prepareCalled.Load() {
		t.Error("OnPrepare should not be called during validation")
	}
}

func TestReloader_Stats(t *testing.T) {
	reloader := NewReloader(&ReloaderConfig{
		ConfigPath: "/test/config",
	})

	stats := reloader.Stats()

	if stats["version"].(int) != 0 {
		t.Errorf("expected version 0, got %v", stats["version"])
	}

	if stats["config_path"].(string) != "/test/config" {
		t.Errorf("expected config_path '/test/config', got %v", stats["config_path"])
	}

	// Reload to increment version
	ctx := context.Background()
	reloader.Reload(ctx)

	stats = reloader.Stats()
	if stats["version"].(int) != 1 {
		t.Errorf("expected version 1, got %v", stats["version"])
	}
}

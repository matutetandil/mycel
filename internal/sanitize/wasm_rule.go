package sanitize

import (
	"context"
	"fmt"
	"sync"

	"github.com/matutetandil/mycel/internal/wasm"
)

// WASMRule is a sanitization rule backed by a WebAssembly module.
// The WASM function receives JSON bytes and returns sanitized JSON bytes.
// Return (0, 0) to signal rejection (input should be blocked).
type WASMRule struct {
	name       string
	wasmPath   string
	entrypoint string
	module     *wasm.Module
	mu         sync.RWMutex
}

// wasmRuntime is the shared WASM runtime for sanitizer modules.
var (
	wasmRuntime     *wasm.Runtime
	wasmRuntimeOnce sync.Once
	wasmRuntimeErr  error
)

// getWASMRuntime returns the shared WASM runtime, creating it if necessary.
func getWASMRuntime() (*wasm.Runtime, error) {
	wasmRuntimeOnce.Do(func() {
		wasmRuntime, wasmRuntimeErr = wasm.NewRuntime(context.Background())
	})
	return wasmRuntime, wasmRuntimeErr
}

// NewWASMRule creates a WASM-backed sanitization rule.
func NewWASMRule(name, wasmPath, entrypoint string) (*WASMRule, error) {
	if wasmPath == "" {
		return nil, fmt.Errorf("wasm path is required for sanitizer %q", name)
	}

	if entrypoint == "" {
		entrypoint = "sanitize"
	}

	r := &WASMRule{
		name:       name,
		wasmPath:   wasmPath,
		entrypoint: entrypoint,
	}

	if err := r.loadModule(); err != nil {
		return nil, err
	}

	return r, nil
}

// loadModule loads the WASM module.
func (r *WASMRule) loadModule() error {
	runtime, err := getWASMRuntime()
	if err != nil {
		return fmt.Errorf("failed to get WASM runtime: %w", err)
	}

	module, err := runtime.LoadModule("sanitizer_"+r.name, r.wasmPath)
	if err != nil {
		return fmt.Errorf("failed to load WASM sanitizer module %q: %w", r.name, err)
	}

	if !module.HasFunction(r.entrypoint) {
		return fmt.Errorf("WASM sanitizer module %q does not export function %q", r.name, r.entrypoint)
	}

	r.module = module
	return nil
}

func (r *WASMRule) Name() string { return r.name }

func (r *WASMRule) Sanitize(value interface{}) (interface{}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.module == nil {
		return nil, fmt.Errorf("WASM sanitizer module %q not loaded", r.name)
	}

	// Call the WASM sanitize function (handles JSON serialization internally)
	result, err := r.module.CallFunction(r.entrypoint, value)
	if err != nil {
		return nil, fmt.Errorf("WASM sanitizer %q failed: %w", r.name, err)
	}

	// Nil result means rejection
	if result == nil {
		return nil, fmt.Errorf("WASM sanitizer %q rejected the input", r.name)
	}

	return result, nil
}

// Reload reloads the WASM module (for hot reload).
func (r *WASMRule) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	runtime, err := getWASMRuntime()
	if err != nil {
		return err
	}

	if err := runtime.UnloadModule("sanitizer_" + r.name); err != nil {
		return err
	}

	r.module = nil
	return r.loadModule()
}

// CloseWASMRuntime closes the shared WASM runtime for sanitizers.
func CloseWASMRuntime() error {
	if wasmRuntime != nil {
		return wasmRuntime.Close()
	}
	return nil
}

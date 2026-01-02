package validator

import (
	"context"
	"fmt"
	"sync"

	"github.com/matutetandil/mycel/internal/wasm"
)

// WASMValidator validates values using a WebAssembly module.
type WASMValidator struct {
	name       string
	wasmPath   string
	entrypoint string
	message    string
	module     *wasm.Module
	mu         sync.RWMutex
}

// wasmRuntime is the shared WASM runtime for all validators.
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

// NewWASMValidator creates a new WASM validator.
func NewWASMValidator(name, wasmPath, entrypoint, message string) (*WASMValidator, error) {
	if wasmPath == "" {
		return nil, fmt.Errorf("wasm path is required")
	}

	if entrypoint == "" {
		entrypoint = "validate"
	}

	if message == "" {
		message = "validation failed"
	}

	v := &WASMValidator{
		name:       name,
		wasmPath:   wasmPath,
		entrypoint: entrypoint,
		message:    message,
	}

	// Load the module
	if err := v.loadModule(); err != nil {
		return nil, err
	}

	return v, nil
}

// loadModule loads the WASM module.
func (v *WASMValidator) loadModule() error {
	runtime, err := getWASMRuntime()
	if err != nil {
		return fmt.Errorf("failed to get WASM runtime: %w", err)
	}

	module, err := runtime.LoadModule(v.name, v.wasmPath)
	if err != nil {
		return fmt.Errorf("failed to load WASM module: %w", err)
	}

	// Verify the module has the required function
	if !module.HasFunction(v.entrypoint) {
		return fmt.Errorf("WASM module does not export function %q", v.entrypoint)
	}

	v.module = module
	return nil
}

func (v *WASMValidator) Name() string {
	return v.name
}

func (v *WASMValidator) Type() ValidatorType {
	return ValidatorTypeWASM
}

func (v *WASMValidator) Validate(value interface{}) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.module == nil {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       "WASM module not loaded",
		}
	}

	err := v.module.CallValidate(v.entrypoint, value)
	if err != nil {
		return &ValidationError{
			ValidatorName: v.name,
			Value:         value,
			Message:       v.message,
		}
	}

	return nil
}

// Reload reloads the WASM module (for hot reload).
func (v *WASMValidator) Reload() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	runtime, err := getWASMRuntime()
	if err != nil {
		return err
	}

	// Unload existing module
	if err := runtime.UnloadModule(v.name); err != nil {
		return err
	}

	v.module = nil
	return v.loadModule()
}

// CloseWASMRuntime closes the shared WASM runtime.
// Should be called when the application shuts down.
func CloseWASMRuntime() error {
	if wasmRuntime != nil {
		return wasmRuntime.Close()
	}
	return nil
}

// Package wasm provides WebAssembly runtime support for Mycel.
// Uses wazero for pure Go WASM execution (no CGO).
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Runtime manages WASM module execution.
type Runtime struct {
	ctx     context.Context
	runtime wazero.Runtime
	modules map[string]*Module
	mu      sync.RWMutex
}

// Module represents a loaded WASM module.
type Module struct {
	name     string
	path     string
	compiled wazero.CompiledModule
	instance api.Module
	runtime  *Runtime
}

// NewRuntime creates a new WASM runtime.
func NewRuntime(ctx context.Context) (*Runtime, error) {
	// Create wazero runtime with default configuration
	r := wazero.NewRuntime(ctx)

	// Instantiate WASI for modules that need it
	_, err := wasi_snapshot_preview1.Instantiate(ctx, r)
	if err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	return &Runtime{
		ctx:     ctx,
		runtime: r,
		modules: make(map[string]*Module),
	}, nil
}

// LoadModule loads a WASM module from a file.
func (r *Runtime) LoadModule(name, path string) (*Module, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already loaded
	if m, ok := r.modules[name]; ok {
		return m, nil
	}

	// Read WASM file
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM file: %w", err)
	}

	// Compile the module
	compiled, err := r.runtime.CompileModule(r.ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WASM module: %w", err)
	}

	// Instantiate the module
	instance, err := r.runtime.InstantiateModule(r.ctx, compiled, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	module := &Module{
		name:     name,
		path:     path,
		compiled: compiled,
		instance: instance,
		runtime:  r,
	}

	r.modules[name] = module
	return module, nil
}

// GetModule returns a loaded module by name.
func (r *Runtime) GetModule(name string) (*Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules[name]
	return m, ok
}

// UnloadModule unloads a module.
func (r *Runtime) UnloadModule(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.modules[name]
	if !ok {
		return nil
	}

	if err := m.instance.Close(r.ctx); err != nil {
		return fmt.Errorf("failed to close module: %w", err)
	}

	delete(r.modules, name)
	return nil
}

// Close closes the runtime and all loaded modules.
func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, m := range r.modules {
		if err := m.instance.Close(r.ctx); err != nil {
			return fmt.Errorf("failed to close module %s: %w", name, err)
		}
	}
	r.modules = make(map[string]*Module)

	return r.runtime.Close(r.ctx)
}

// CallFunction calls a function in the module with JSON input/output.
func (m *Module) CallFunction(name string, input interface{}) (interface{}, error) {
	// Get the function
	fn := m.instance.ExportedFunction(name)
	if fn == nil {
		return nil, fmt.Errorf("function %q not found in module %s", name, m.name)
	}

	// Serialize input to JSON
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	// Allocate memory for input
	inputPtr, err := m.allocate(uint32(len(inputJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to allocate input memory: %w", err)
	}
	defer m.free(inputPtr, uint32(len(inputJSON)))

	// Write input to memory
	if !m.instance.Memory().Write(inputPtr, inputJSON) {
		return nil, fmt.Errorf("failed to write input to memory")
	}

	// Call the function
	results, err := fn.Call(m.runtime.ctx, uint64(inputPtr), uint64(len(inputJSON)))
	if err != nil {
		return nil, fmt.Errorf("function call failed: %w", err)
	}

	// Parse results - expecting (ptr, len) tuple
	if len(results) < 2 {
		// Single return value (e.g., status code)
		if len(results) == 1 {
			return results[0], nil
		}
		return nil, nil
	}

	resultPtr := uint32(results[0])
	resultLen := uint32(results[1])

	if resultLen == 0 {
		return nil, nil
	}

	// Read result from memory
	resultJSON, ok := m.instance.Memory().Read(resultPtr, resultLen)
	if !ok {
		return nil, fmt.Errorf("failed to read result from memory")
	}

	// Free result memory
	m.free(resultPtr, resultLen)

	// Parse result JSON
	var result interface{}
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return result, nil
}

// CallValidate calls a validate function that returns a status code.
// Returns nil if valid (status 0), or an error if invalid.
func (m *Module) CallValidate(fnName string, value interface{}) error {
	// Get the function
	fn := m.instance.ExportedFunction(fnName)
	if fn == nil {
		return fmt.Errorf("function %q not found in module %s", fnName, m.name)
	}

	// Serialize value to JSON
	valueJSON, err := json.Marshal(map[string]interface{}{"value": value})
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	// Allocate memory for input
	inputPtr, err := m.allocate(uint32(len(valueJSON)))
	if err != nil {
		return fmt.Errorf("failed to allocate input memory: %w", err)
	}
	defer m.free(inputPtr, uint32(len(valueJSON)))

	// Write input to memory
	if !m.instance.Memory().Write(inputPtr, valueJSON) {
		return fmt.Errorf("failed to write input to memory")
	}

	// Call the function
	results, err := fn.Call(m.runtime.ctx, uint64(inputPtr), uint64(len(valueJSON)))
	if err != nil {
		return fmt.Errorf("validation call failed: %w", err)
	}

	// Check result
	if len(results) == 0 {
		return nil // Assume success if no return
	}

	status := results[0]
	if status == 0 {
		return nil // Valid
	}

	// Invalid - status is either 1 (use default message) or a pointer to error message
	if status == 1 {
		return fmt.Errorf("validation failed")
	}

	// Try to read error message from memory (if status > 1, it might be a pointer)
	// For simplicity, we'll just return a generic error
	return fmt.Errorf("validation failed (status: %d)", status)
}

// allocate allocates memory in the WASM module.
func (m *Module) allocate(size uint32) (uint32, error) {
	allocFn := m.instance.ExportedFunction("alloc")
	if allocFn == nil {
		// If no alloc function, try malloc
		allocFn = m.instance.ExportedFunction("malloc")
	}
	if allocFn == nil {
		return 0, fmt.Errorf("module %s does not export alloc or malloc function", m.name)
	}

	results, err := allocFn.Call(m.runtime.ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("alloc failed: %w", err)
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("alloc returned no result")
	}

	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, fmt.Errorf("alloc returned null pointer")
	}

	return ptr, nil
}

// free frees memory in the WASM module.
func (m *Module) free(ptr, size uint32) {
	freeFn := m.instance.ExportedFunction("free")
	if freeFn == nil {
		freeFn = m.instance.ExportedFunction("dealloc")
	}
	if freeFn == nil {
		return // No free function, assume GC or manual management
	}

	// Try calling with (ptr, size) first, then just (ptr)
	_, _ = freeFn.Call(m.runtime.ctx, uint64(ptr), uint64(size))
}

// HasFunction checks if the module exports a function.
func (m *Module) HasFunction(name string) bool {
	return m.instance.ExportedFunction(name) != nil
}

// Name returns the module name.
func (m *Module) Name() string {
	return m.name
}

// Path returns the module file path.
func (m *Module) Path() string {
	return m.path
}

// Package functions provides WASM-based custom functions for CEL expressions.
package functions

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/matutetandil/mycel/internal/wasm"
)

// Config holds the configuration for a WASM functions module.
type Config struct {
	Name    string   // Logical name of the functions module
	WASM    string   // Path to .wasm file
	Exports []string // Function names to export
}

// Registry manages loaded WASM function modules.
type Registry struct {
	modules map[string]*Module
	mu      sync.RWMutex
}

// Module represents a loaded WASM functions module.
type Module struct {
	name       string
	wasmModule *wasm.Module
	exports    []string
}

// NewRegistry creates a new functions registry.
func NewRegistry() *Registry {
	return &Registry{
		modules: make(map[string]*Module),
	}
}

// wasmRuntime is the shared WASM runtime for all function modules.
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

// Register loads and registers a WASM functions module.
func (r *Registry) Register(cfg *Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get or create WASM runtime
	runtime, err := getWASMRuntime()
	if err != nil {
		return fmt.Errorf("failed to get WASM runtime: %w", err)
	}

	// Load the WASM module
	moduleName := "functions_" + cfg.Name
	wasmModule, err := runtime.LoadModule(moduleName, cfg.WASM)
	if err != nil {
		return fmt.Errorf("failed to load WASM module %s: %w", cfg.WASM, err)
	}

	// Verify all exported functions exist
	for _, fnName := range cfg.Exports {
		if !wasmModule.HasFunction(fnName) {
			return fmt.Errorf("WASM module %s does not export function %q", cfg.WASM, fnName)
		}
	}

	module := &Module{
		name:       cfg.Name,
		wasmModule: wasmModule,
		exports:    cfg.Exports,
	}

	r.modules[cfg.Name] = module
	return nil
}

// GetFunction returns a callable wrapper for a WASM function.
func (r *Registry) GetFunction(moduleName, fnName string) (Function, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	module, ok := r.modules[moduleName]
	if !ok {
		return nil, fmt.Errorf("functions module %q not found", moduleName)
	}

	// Verify function is in exports list
	found := false
	for _, exp := range module.exports {
		if exp == fnName {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("function %q not exported by module %q", fnName, moduleName)
	}

	return &wasmFunction{
		name:   fnName,
		module: module.wasmModule,
	}, nil
}

// GetAllFunctions returns all registered functions across all modules.
// Returns map[functionName]Function where functionName is the exported name.
func (r *Registry) GetAllFunctions() map[string]Function {
	r.mu.RLock()
	defer r.mu.RUnlock()

	funcs := make(map[string]Function)
	for _, module := range r.modules {
		for _, fnName := range module.exports {
			funcs[fnName] = &wasmFunction{
				name:   fnName,
				module: module.wasmModule,
			}
		}
	}
	return funcs
}

// Function is the interface for callable WASM functions.
type Function interface {
	// Name returns the function name.
	Name() string
	// Call invokes the function with the given arguments.
	Call(args ...interface{}) (interface{}, error)
}

// wasmFunction wraps a WASM function for CEL integration.
type wasmFunction struct {
	name   string
	module *wasm.Module
}

func (f *wasmFunction) Name() string {
	return f.name
}

func (f *wasmFunction) Call(args ...interface{}) (interface{}, error) {
	// Prepare input as JSON
	input := map[string]interface{}{
		"args": args,
	}

	// Call WASM function
	result, err := f.module.CallFunction(f.name, input)
	if err != nil {
		return nil, fmt.Errorf("WASM function %s call failed: %w", f.name, err)
	}

	// Parse result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		// If not a map, return directly
		return result, nil
	}

	// Check for error
	if errVal, ok := resultMap["error"]; ok && errVal != nil {
		if errStr, ok := errVal.(string); ok && errStr != "" {
			return nil, fmt.Errorf("WASM function %s error: %s", f.name, errStr)
		}
	}

	// Return result value
	if val, ok := resultMap["result"]; ok {
		return val, nil
	}

	return result, nil
}

// CallWithJSON invokes a WASM function with raw JSON arguments.
func (f *wasmFunction) CallWithJSON(argsJSON []byte) ([]byte, error) {
	var args []interface{}
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	result, err := f.Call(args...)
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

// Close closes the registry and releases resources.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.modules = make(map[string]*Module)
	return nil
}

// ModuleNames returns the names of all registered modules.
func (r *Registry) ModuleNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	return names
}

// FunctionNames returns the names of all functions in a module.
func (r *Registry) FunctionNames(moduleName string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	module, ok := r.modules[moduleName]
	if !ok {
		return nil, fmt.Errorf("module %q not found", moduleName)
	}

	return module.exports, nil
}

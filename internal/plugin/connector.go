package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/wasm"
)

// WASMConnector implements connector.Connector using a WASM module.
type WASMConnector struct {
	name       string
	typeName   string
	wasmPath   string
	config     map[string]interface{}
	module     *wasm.Module
	mu         sync.RWMutex
	connected  bool
}

// wasmRuntime is the shared WASM runtime for all plugin connectors.
var (
	wasmRuntime     *wasm.Runtime
	wasmRuntimeOnce sync.Once
	wasmRuntimeErr  error
)

// getWASMRuntime returns the shared WASM runtime.
func getWASMRuntime() (*wasm.Runtime, error) {
	wasmRuntimeOnce.Do(func() {
		wasmRuntime, wasmRuntimeErr = wasm.NewRuntime(context.Background())
	})
	return wasmRuntime, wasmRuntimeErr
}

// NewWASMConnector creates a new WASM-based connector.
func NewWASMConnector(name, typeName, wasmPath string, config map[string]interface{}) (*WASMConnector, error) {
	if wasmPath == "" {
		return nil, fmt.Errorf("wasm path is required")
	}

	return &WASMConnector{
		name:     name,
		typeName: typeName,
		wasmPath: wasmPath,
		config:   config,
	}, nil
}

// Name returns the connector name.
func (c *WASMConnector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *WASMConnector) Type() string {
	return c.typeName
}

// Connect initializes the WASM module and calls init().
func (c *WASMConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Get WASM runtime
	runtime, err := getWASMRuntime()
	if err != nil {
		return fmt.Errorf("failed to get WASM runtime: %w", err)
	}

	// Load the WASM module
	moduleName := "plugin_" + c.typeName + "_" + c.name
	module, err := runtime.LoadModule(moduleName, c.wasmPath)
	if err != nil {
		return fmt.Errorf("failed to load WASM module: %w", err)
	}
	c.module = module

	// Call init() if the module exports it
	if module.HasFunction("init") {
		configJSON, err := json.Marshal(c.config)
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		result, err := c.callWASM("init", configJSON)
		if err != nil {
			return fmt.Errorf("init failed: %w", err)
		}

		// Check for error in result
		if resultMap, ok := result.(map[string]interface{}); ok {
			if errVal, ok := resultMap["error"]; ok && errVal != nil && errVal != "" {
				return fmt.Errorf("init error: %v", errVal)
			}
		}
	}

	c.connected = true
	return nil
}

// Close calls close() on the WASM module.
func (c *WASMConnector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.module == nil {
		return nil
	}

	// Call close() if the module exports it
	if c.module.HasFunction("close") {
		_, _ = c.callWASM("close", nil)
	}

	c.connected = false
	return nil
}

// Health checks if the connector is healthy.
func (c *WASMConnector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.module == nil {
		return fmt.Errorf("connector not connected")
	}

	// Call health() if the module exports it
	if c.module.HasFunction("health") {
		result, err := c.callWASM("health", nil)
		if err != nil {
			return fmt.Errorf("health check failed: %w", err)
		}

		// Check for error in result
		if resultMap, ok := result.(map[string]interface{}); ok {
			if errVal, ok := resultMap["error"]; ok && errVal != nil && errVal != "" {
				return fmt.Errorf("unhealthy: %v", errVal)
			}
		}
	}

	return nil
}

// Read implements connector.Reader.
func (c *WASMConnector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.module == nil {
		return nil, fmt.Errorf("connector not connected")
	}

	// Build query JSON
	queryJSON, err := json.Marshal(map[string]interface{}{
		"target":     query.Target,
		"operation":  query.Operation,
		"filters":    query.Filters,
		"fields":     query.Fields,
		"pagination": query.Pagination,
		"order_by":   query.OrderBy,
		"params":     query.Params,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	// Call read()
	result, err := c.callWASM("read", queryJSON)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	return c.parseResult(result)
}

// Write implements connector.Writer.
func (c *WASMConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.module == nil {
		return nil, fmt.Errorf("connector not connected")
	}

	// Build data JSON
	dataJSON, err := json.Marshal(map[string]interface{}{
		"target":    data.Target,
		"operation": data.Operation,
		"payload":   data.Payload,
		"filters":   data.Filters,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	// Call write()
	result, err := c.callWASM("write", dataJSON)
	if err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}

	return c.parseResult(result)
}

// Call implements connector.Caller for RPC-style operations.
func (c *WASMConnector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.module == nil {
		return nil, fmt.Errorf("connector not connected")
	}

	// Build call JSON
	callJSON, err := json.Marshal(map[string]interface{}{
		"operation": operation,
		"params":    params,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal call: %w", err)
	}

	// Call call()
	result, err := c.callWASM("call", callJSON)
	if err != nil {
		return nil, fmt.Errorf("call failed: %w", err)
	}

	// Parse result
	if resultMap, ok := result.(map[string]interface{}); ok {
		if errVal, ok := resultMap["error"]; ok && errVal != nil && errVal != "" {
			return nil, fmt.Errorf("%v", errVal)
		}
		if data, ok := resultMap["data"]; ok {
			return data, nil
		}
		if res, ok := resultMap["result"]; ok {
			return res, nil
		}
	}

	return result, nil
}

// callWASM calls a WASM function with the given input.
func (c *WASMConnector) callWASM(fnName string, input []byte) (interface{}, error) {
	if !c.module.HasFunction(fnName) {
		return nil, fmt.Errorf("function %q not found in WASM module", fnName)
	}

	// Call with raw JSON input
	var inputData interface{}
	if input != nil {
		if err := json.Unmarshal(input, &inputData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal input: %w", err)
		}
	}

	return c.module.CallFunction(fnName, inputData)
}

// parseResult converts WASM result to connector.Result.
func (c *WASMConnector) parseResult(result interface{}) (*connector.Result, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		// Simple result - wrap in rows array
		return &connector.Result{
			Rows:     []map[string]interface{}{{"result": result}},
			Affected: 1,
		}, nil
	}

	// Check for error
	if errVal, ok := resultMap["error"]; ok && errVal != nil && errVal != "" {
		return nil, fmt.Errorf("%v", errVal)
	}

	connResult := &connector.Result{}

	// Extract data (stored in Rows)
	if data, ok := resultMap["data"]; ok {
		switch d := data.(type) {
		case []interface{}:
			connResult.Rows = make([]map[string]interface{}, len(d))
			for i, item := range d {
				if m, ok := item.(map[string]interface{}); ok {
					connResult.Rows[i] = m
				}
			}
		case map[string]interface{}:
			connResult.Rows = []map[string]interface{}{d}
		}
	}

	// Extract metadata
	if metadata, ok := resultMap["metadata"].(map[string]interface{}); ok {
		if affected, ok := metadata["affected"].(float64); ok {
			connResult.Affected = int64(affected)
		}
		if id, ok := metadata["id"]; ok {
			connResult.LastID = id
		}
	}

	// Direct affected count
	if affected, ok := resultMap["affected"].(float64); ok {
		connResult.Affected = int64(affected)
	}

	return connResult, nil
}

// Ensure WASMConnector implements the required interfaces.
var (
	_ connector.Connector  = (*WASMConnector)(nil)
	_ connector.Reader     = (*WASMConnector)(nil)
	_ connector.Writer     = (*WASMConnector)(nil)
)

package mock

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/transform"
)

// Connector wraps a real connector with mock capabilities.
type Connector struct {
	name     string
	real     connector.Connector
	loader   *Loader
	config   *ConnectorMockConfig
	cel      *transform.CELTransformer
	logger   *slog.Logger
	mockOnly bool // If true, always return mock (don't fallback to real)
}

// NewConnector creates a mock-wrapped connector.
func NewConnector(name string, real connector.Connector, loader *Loader, config *ConnectorMockConfig, mockOnly bool) (*Connector, error) {
	cel, err := transform.NewCELTransformer()
	if err != nil {
		return nil, fmt.Errorf("creating CEL transformer for mock: %w", err)
	}

	return &Connector{
		name:     name,
		real:     real,
		loader:   loader,
		config:   config,
		cel:      cel,
		logger:   slog.Default().With("component", "mock", "connector", name),
		mockOnly: mockOnly,
	}, nil
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the underlying connector type.
func (c *Connector) Type() string {
	if c.real != nil {
		return c.real.Type()
	}
	return "mock"
}

// Read implements connector.Reader.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// Try to find mock
	result, err := c.findMock(ctx, query.Target, nil)
	if err != nil {
		return nil, err
	}

	if result.Found {
		c.logger.Debug("mock hit",
			"target", query.Target,
			"affected", result.Affected)

		// Apply latency
		c.applyLatency(result.Delay)

		if result.Error != nil {
			return nil, result.Error
		}

		return c.toConnectorResult(result), nil
	}

	// No mock found
	if c.mockOnly {
		return &connector.Result{
			Rows: []map[string]interface{}{},
		}, nil
	}

	// Fallback to real connector
	if reader, ok := c.real.(connector.Reader); ok {
		return reader.Read(ctx, query)
	}

	return nil, fmt.Errorf("connector %s does not support read operations", c.name)
}

// Write implements connector.Writer.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Try to find mock
	result, err := c.findMock(ctx, data.Target, data.Payload)
	if err != nil {
		return nil, err
	}

	if result.Found {
		c.logger.Debug("mock hit (write)",
			"target", data.Target,
			"operation", data.Operation,
			"affected", result.Affected)

		c.applyLatency(result.Delay)

		if result.Error != nil {
			return nil, result.Error
		}

		return c.toConnectorResult(result), nil
	}

	// No mock found
	if c.mockOnly {
		return &connector.Result{
			Affected: 1, // Simulate successful write
		}, nil
	}

	// Fallback to real connector
	if writer, ok := c.real.(connector.Writer); ok {
		return writer.Write(ctx, data)
	}

	return nil, fmt.Errorf("connector %s does not support write operations", c.name)
}

// Call implements connector operations.
func (c *Connector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	// For Call operations, try loading operation-specific mock
	mock, err := c.loader.LoadOperationMock(c.name, "CALL", operation)
	if err != nil {
		return nil, err
	}

	if mock != nil {
		result, err := c.evaluateMock(ctx, mock, params)
		if err != nil {
			return nil, err
		}

		if result.Found {
			c.logger.Debug("mock hit (call)",
				"operation", operation)

			c.applyLatency(result.Delay)

			if result.Error != nil {
				return nil, result.Error
			}

			return result.Data, nil
		}
	}

	// No mock found
	if c.mockOnly {
		return nil, nil
	}

	// Try real connector
	if caller, ok := c.real.(interface {
		Call(context.Context, string, map[string]interface{}) (interface{}, error)
	}); ok {
		return caller.Call(ctx, operation, params)
	}

	return nil, fmt.Errorf("connector %s does not support call operations", c.name)
}

// Connect establishes the connection (delegates to real connector).
func (c *Connector) Connect(ctx context.Context) error {
	if c.real != nil {
		return c.real.Connect(ctx)
	}
	return nil
}

// Close closes the underlying connector.
func (c *Connector) Close(ctx context.Context) error {
	if c.real != nil {
		return c.real.Close(ctx)
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if c.real != nil {
		return c.real.Health(ctx)
	}
	return nil
}

// findMock looks for a mock file and evaluates conditions.
func (c *Connector) findMock(ctx context.Context, target string, input map[string]interface{}) (*MockResult, error) {
	mock, err := c.loader.LoadConnectorMock(c.name, target)
	if err != nil {
		return nil, err
	}

	if mock == nil {
		return &MockResult{Found: false}, nil
	}

	return c.evaluateMock(ctx, mock, input)
}

// evaluateMock evaluates a mock file and returns the appropriate response.
func (c *Connector) evaluateMock(ctx context.Context, mock *MockFile, input map[string]interface{}) (*MockResult, error) {
	// If no conditional responses, use simple data
	if len(mock.Responses) == 0 {
		return &MockResult{
			Found:    true,
			Data:     mock.Data,
			Affected: mock.Affected,
		}, nil
	}

	// Prepare input for CEL evaluation
	if input == nil {
		input = make(map[string]interface{})
	}

	// Evaluate conditional responses
	var defaultResponse *ConditionalResponse
	for i := range mock.Responses {
		resp := &mock.Responses[i]

		if resp.Default {
			defaultResponse = resp
			continue
		}

		if resp.When != "" {
			// Evaluate CEL condition
			result, err := c.cel.EvaluateExpression(ctx, input, nil, resp.When)
			if err != nil {
				c.logger.Warn("mock condition evaluation error",
					"condition", resp.When,
					"error", err)
				continue
			}

			if boolVal, ok := result.(bool); ok && boolVal {
				return c.responseToResult(resp), nil
			}
		}
	}

	// Use default response if available
	if defaultResponse != nil {
		return c.responseToResult(defaultResponse), nil
	}

	// No matching condition
	return &MockResult{Found: false}, nil
}

// responseToResult converts a ConditionalResponse to MockResult.
func (c *Connector) responseToResult(resp *ConditionalResponse) *MockResult {
	result := &MockResult{
		Found:    true,
		Data:     resp.Data,
		Affected: resp.Affected,
		Status:   resp.Status,
	}

	if resp.Error != "" {
		result.Error = fmt.Errorf("%s", resp.Error)
	}

	if resp.Delay != "" {
		if d, err := time.ParseDuration(resp.Delay); err == nil {
			result.Delay = d
		}
	}

	return result
}

// toConnectorResult converts MockResult to connector.Result.
func (c *Connector) toConnectorResult(mr *MockResult) *connector.Result {
	result := &connector.Result{
		Affected: mr.Affected,
	}

	// Convert data to rows
	switch v := mr.Data.(type) {
	case []interface{}:
		for _, item := range v {
			if row, ok := item.(map[string]interface{}); ok {
				result.Rows = append(result.Rows, row)
			}
		}
	case map[string]interface{}:
		result.Rows = []map[string]interface{}{v}
	case []map[string]interface{}:
		result.Rows = v
	}

	return result
}

// applyLatency applies configured and per-response latency.
func (c *Connector) applyLatency(extra time.Duration) {
	total := extra
	if c.config != nil && c.config.Latency > 0 {
		total += c.config.Latency
	}
	if total > 0 {
		time.Sleep(total)
	}
}

// Unwrap returns the underlying real connector.
func (c *Connector) Unwrap() connector.Connector {
	return c.real
}

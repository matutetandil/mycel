// Package mock provides mock connector functionality for testing.
package mock

import (
	"time"
)

// Config holds mock system configuration.
type Config struct {
	// Enabled indicates if mocking is active.
	Enabled bool

	// Path is the directory containing mock files.
	Path string

	// Connectors contains per-connector mock settings.
	Connectors map[string]*ConnectorMockConfig

	// MockOnly lists connectors to mock (empty = all when enabled).
	MockOnly []string

	// NoMock lists connectors to exclude from mocking.
	NoMock []string
}

// ConnectorMockConfig holds per-connector mock settings.
type ConnectorMockConfig struct {
	// Latency simulates network/processing delay.
	Latency time.Duration

	// FailRate is the percentage of requests that should fail (0-100).
	FailRate int

	// Enabled can disable mocking for this specific connector.
	Enabled *bool
}

// MockFile represents the structure of a mock JSON file.
type MockFile struct {
	// Simple response (no conditions)
	Data     interface{}            `json:"data,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Affected int64                  `json:"affected,omitempty"`

	// Conditional responses
	Responses []ConditionalResponse `json:"responses,omitempty"`
}

// ConditionalResponse is a response with an optional condition.
type ConditionalResponse struct {
	// When is a CEL expression that must evaluate to true.
	When string `json:"when,omitempty"`

	// Default indicates this is the fallback response.
	Default bool `json:"default,omitempty"`

	// Data is the response data.
	Data interface{} `json:"data,omitempty"`

	// Affected is the number of affected rows (for write operations).
	Affected int64 `json:"affected,omitempty"`

	// Error simulates an error response.
	Error string `json:"error,omitempty"`

	// Status is the HTTP status code (for REST mocks).
	Status int `json:"status,omitempty"`

	// Delay adds additional latency for this specific response.
	Delay string `json:"delay,omitempty"`
}

// MockResult represents the result of a mock lookup.
type MockResult struct {
	// Data is the mock response data.
	Data interface{}

	// Affected is the number of affected rows.
	Affected int64

	// Error is set if the mock simulates an error.
	Error error

	// Status is the HTTP status code (if applicable).
	Status int

	// Delay is additional latency to simulate.
	Delay time.Duration

	// Found indicates if a mock was found.
	Found bool
}

// Package errors provides domain-specific error types for Mycel.
package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for common error conditions
var (
	// ErrNotFound indicates a resource was not found
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists indicates a resource already exists
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidConfig indicates invalid configuration
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrConnection indicates a connection error
	ErrConnection = errors.New("connection error")

	// ErrTimeout indicates an operation timed out
	ErrTimeout = errors.New("operation timed out")

	// ErrValidation indicates a validation error
	ErrValidation = errors.New("validation error")

	// ErrNotSupported indicates an unsupported operation
	ErrNotSupported = errors.New("not supported")
)

// ConfigError represents a configuration-related error
type ConfigError struct {
	File    string
	Line    int
	Column  int
	Message string
	Cause   error
}

func (e *ConfigError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

func (e *ConfigError) Unwrap() error {
	return e.Cause
}

// NewConfigError creates a new configuration error
func NewConfigError(file, message string) *ConfigError {
	return &ConfigError{
		File:    file,
		Message: message,
	}
}

// NewConfigErrorAt creates a new configuration error with location
func NewConfigErrorAt(file string, line, column int, message string) *ConfigError {
	return &ConfigError{
		File:    file,
		Line:    line,
		Column:  column,
		Message: message,
	}
}

// ConnectorError represents a connector-related error
type ConnectorError struct {
	Connector string
	Operation string
	Message   string
	Cause     error
}

func (e *ConnectorError) Error() string {
	if e.Operation != "" {
		return fmt.Sprintf("connector %s: %s: %s", e.Connector, e.Operation, e.Message)
	}
	return fmt.Sprintf("connector %s: %s", e.Connector, e.Message)
}

func (e *ConnectorError) Unwrap() error {
	return e.Cause
}

// NewConnectorError creates a new connector error
func NewConnectorError(connector, message string) *ConnectorError {
	return &ConnectorError{
		Connector: connector,
		Message:   message,
	}
}

// NewConnectorOpError creates a new connector error for a specific operation
func NewConnectorOpError(connector, operation, message string, cause error) *ConnectorError {
	return &ConnectorError{
		Connector: connector,
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

// FlowError represents a flow execution error
type FlowError struct {
	Flow    string
	Stage   string
	Message string
	Cause   error
}

func (e *FlowError) Error() string {
	if e.Stage != "" {
		return fmt.Sprintf("flow %s [%s]: %s", e.Flow, e.Stage, e.Message)
	}
	return fmt.Sprintf("flow %s: %s", e.Flow, e.Message)
}

func (e *FlowError) Unwrap() error {
	return e.Cause
}

// NewFlowError creates a new flow error
func NewFlowError(flow, message string) *FlowError {
	return &FlowError{
		Flow:    flow,
		Message: message,
	}
}

// NewFlowStageError creates a new flow error for a specific stage
func NewFlowStageError(flow, stage, message string, cause error) *FlowError {
	return &FlowError{
		Flow:    flow,
		Stage:   stage,
		Message: message,
		Cause:   cause,
	}
}

// ValidationError represents a data validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
	Code    string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message, code string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Code:    code,
	}
}

// ValidationErrors is a collection of validation errors
type ValidationErrors struct {
	Errors []*ValidationError
}

func (e *ValidationErrors) Error() string {
	if len(e.Errors) == 0 {
		return "no validation errors"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("%d validation errors: %s (and %d more)",
		len(e.Errors), e.Errors[0].Error(), len(e.Errors)-1)
}

// Add adds a validation error to the collection
func (e *ValidationErrors) Add(err *ValidationError) {
	e.Errors = append(e.Errors, err)
}

// HasErrors returns true if there are any validation errors
func (e *ValidationErrors) HasErrors() bool {
	return len(e.Errors) > 0
}

// Is checks if the error matches the target error
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// Wrap wraps an error with additional context
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

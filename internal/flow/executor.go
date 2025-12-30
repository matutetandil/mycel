package flow

import (
	"context"
	"fmt"
)

// Stage represents a single step in the flow pipeline.
// Single Responsibility Principle - each stage does ONE thing.
type Stage interface {
	// Name returns the stage identifier.
	Name() string

	// Execute processes the stage data and returns the result.
	Execute(ctx context.Context, data *StageData) (*StageData, error)
}

// StageData passes data between pipeline stages.
type StageData struct {
	// Input is the original flow input.
	Input *Input

	// Output is the accumulated output data.
	Output map[string]interface{}

	// Enriched holds data fetched from external sources via enrich blocks.
	// Keys are enrichment names, values are the fetched data.
	Enriched map[string]interface{}

	// Context is the flow execution context.
	Context *Context

	// Errors accumulated during execution.
	Errors []error

	// Metadata for inter-stage communication.
	Metadata map[string]interface{}
}

// NewStageData creates a new StageData from flow input.
func NewStageData(input *Input, flowCtx *Context) *StageData {
	return &StageData{
		Input:    input,
		Output:   make(map[string]interface{}),
		Enriched: make(map[string]interface{}),
		Context:  flowCtx,
		Errors:   make([]error, 0),
		Metadata: make(map[string]interface{}),
	}
}

// AddError adds an error to the stage data.
func (d *StageData) AddError(err error) {
	d.Errors = append(d.Errors, err)
}

// HasErrors returns true if there are any errors.
func (d *StageData) HasErrors() bool {
	return len(d.Errors) > 0
}

// SetMeta stores metadata for inter-stage communication.
func (d *StageData) SetMeta(key string, value interface{}) {
	d.Metadata[key] = value
}

// GetMeta retrieves metadata.
func (d *StageData) GetMeta(key string) (interface{}, bool) {
	v, ok := d.Metadata[key]
	return v, ok
}

// Executor runs flow pipelines.
// Follows the Pipeline pattern for composable flow execution.
type Executor struct {
	stages []Stage
}

// NewExecutor creates a new flow executor.
func NewExecutor() *Executor {
	return &Executor{
		stages: make([]Stage, 0),
	}
}

// AddStage adds a stage to the pipeline.
func (e *Executor) AddStage(stage Stage) *Executor {
	e.stages = append(e.stages, stage)
	return e
}

// Execute runs all stages in sequence.
func (e *Executor) Execute(ctx context.Context, input *Input, flowCtx *Context) (*Output, error) {
	data := NewStageData(input, flowCtx)

	for _, stage := range e.stages {
		var err error
		data, err = stage.Execute(ctx, data)
		if err != nil {
			return NewErrorOutput(500, err.Error()), fmt.Errorf("stage %s failed: %w", stage.Name(), err)
		}

		// Check for accumulated errors
		if data.HasErrors() {
			// Return first error as the main error
			return NewErrorOutput(400, data.Errors[0].Error()), data.Errors[0]
		}
	}

	return NewOutput(data.Output), nil
}

// StageCount returns the number of stages in the pipeline.
func (e *Executor) StageCount() int {
	return len(e.stages)
}

// ExecutorBuilder provides a fluent API for building executors.
type ExecutorBuilder struct {
	executor *Executor
}

// NewExecutorBuilder creates a new executor builder.
func NewExecutorBuilder() *ExecutorBuilder {
	return &ExecutorBuilder{
		executor: NewExecutor(),
	}
}

// WithStage adds a stage to the executor.
func (b *ExecutorBuilder) WithStage(stage Stage) *ExecutorBuilder {
	b.executor.AddStage(stage)
	return b
}

// Build returns the configured executor.
func (b *ExecutorBuilder) Build() *Executor {
	return b.executor
}

// StageFunc is a function that can be used as a Stage.
type StageFunc struct {
	name string
	fn   func(ctx context.Context, data *StageData) (*StageData, error)
}

// NewStageFunc creates a Stage from a function.
func NewStageFunc(name string, fn func(ctx context.Context, data *StageData) (*StageData, error)) Stage {
	return &StageFunc{name: name, fn: fn}
}

// Name returns the stage name.
func (s *StageFunc) Name() string {
	return s.name
}

// Execute runs the stage function.
func (s *StageFunc) Execute(ctx context.Context, data *StageData) (*StageData, error) {
	return s.fn(ctx, data)
}

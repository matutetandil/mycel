package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/saga"
)

// Engine manages long-running workflow instances with persistence.
type Engine struct {
	store    Store
	executor *saga.Executor
	sagas    map[string]*saga.Config
	logger   *slog.Logger

	// Background ticker
	tickInterval time.Duration
	done         chan struct{}
	running      sync.Map // instanceID → context.CancelFunc
}

// NewEngine creates a new workflow engine.
func NewEngine(store Store, executor *saga.Executor, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		store:        store,
		executor:     executor,
		sagas:        make(map[string]*saga.Config),
		logger:       logger,
		tickInterval: 5 * time.Second,
		done:         make(chan struct{}),
	}
}

// RegisterSaga registers a saga configuration for workflow execution.
func (e *Engine) RegisterSaga(config *saga.Config) {
	e.sagas[config.Name] = config
}

// Start begins the background ticker and resumes any active workflows.
func (e *Engine) Start(ctx context.Context) error {
	// Resume active workflows
	active, err := e.store.FindActive(ctx)
	if err != nil {
		e.logger.Warn("failed to query active workflows", slog.Any("error", err))
	} else if len(active) > 0 {
		e.logger.Info("resuming active workflows", slog.Int("count", len(active)))
		for _, inst := range active {
			if inst.Status == StatusRunning {
				go e.resumeInstance(context.Background(), inst)
			}
			// Paused instances are handled by the ticker (delay/await)
		}
	}

	// Start background ticker
	go e.tickLoop(ctx)

	return nil
}

// Stop stops the background ticker and cancels all active workflows.
func (e *Engine) Stop() {
	close(e.done)
	e.running.Range(func(key, value interface{}) bool {
		if cancel, ok := value.(context.CancelFunc); ok {
			cancel()
		}
		return true
	})
}

// Execute starts a new workflow instance for the given saga.
func (e *Engine) Execute(ctx context.Context, sagaName string, input map[string]interface{}) (*Instance, error) {
	cfg, ok := e.sagas[sagaName]
	if !ok {
		return nil, fmt.Errorf("saga %q not registered", sagaName)
	}

	now := time.Now()
	inst := &Instance{
		ID:          fmt.Sprintf("wf_%d", now.UnixNano()),
		SagaName:    sagaName,
		Status:      StatusRunning,
		CurrentStep: 0,
		Input:       input,
		StepResults: make(map[string]interface{}),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Set saga-level timeout
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			expires := now.Add(d)
			inst.ExpiresAt = &expires
		}
	}

	if err := e.store.Save(ctx, inst); err != nil {
		return nil, fmt.Errorf("failed to save workflow: %w", err)
	}

	// Check if any step needs async execution
	if NeedsPersistence(cfg) {
		go e.runFromStep(context.Background(), inst)
		return inst, nil
	}

	// Synchronous execution for simple sagas
	e.runFromStep(ctx, inst)

	// Reload to get final state
	final, err := e.store.Get(ctx, inst.ID)
	if err != nil {
		return inst, nil
	}
	return final, nil
}

// Signal resumes a paused workflow that is awaiting an event.
func (e *Engine) Signal(ctx context.Context, instanceID, event string, data map[string]interface{}) error {
	inst, err := e.store.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("workflow not found: %w", err)
	}

	if inst.Status != StatusPaused {
		return fmt.Errorf("workflow %s is not paused (status: %s)", instanceID, inst.Status)
	}

	if inst.AwaitEvent != event {
		return fmt.Errorf("workflow %s is awaiting event %q, not %q", instanceID, inst.AwaitEvent, event)
	}

	// Store signal data and resume
	inst.SignalData = data
	inst.AwaitEvent = ""
	inst.StepExpiresAt = nil
	inst.Status = StatusRunning

	if err := e.store.Save(ctx, inst); err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	e.logger.Info("workflow signaled",
		slog.String("id", instanceID),
		slog.String("event", event),
	)

	go e.runFromStep(context.Background(), inst)
	return nil
}

// GetInstance retrieves a workflow instance.
func (e *Engine) GetInstance(ctx context.Context, id string) (*Instance, error) {
	return e.store.Get(ctx, id)
}

// Cancel cancels an active workflow.
func (e *Engine) Cancel(ctx context.Context, id string) error {
	inst, err := e.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if inst.Status != StatusRunning && inst.Status != StatusPaused {
		return fmt.Errorf("workflow %s cannot be cancelled (status: %s)", id, inst.Status)
	}

	// Cancel running context if any
	if cancel, ok := e.running.LoadAndDelete(id); ok {
		cancel.(context.CancelFunc)()
	}

	inst.Status = StatusCancelled
	return e.store.Save(ctx, inst)
}

// runFromStep continues workflow execution from the current step.
func (e *Engine) runFromStep(ctx context.Context, inst *Instance) {
	cfg, ok := e.sagas[inst.SagaName]
	if !ok {
		e.logger.Error("saga config not found for workflow", slog.String("saga", inst.SagaName))
		return
	}

	// Create cancellable context
	runCtx, cancel := context.WithCancel(ctx)
	e.running.Store(inst.ID, cancel)
	defer func() {
		cancel()
		e.running.Delete(inst.ID)
	}()

	// Apply saga-level timeout
	if inst.ExpiresAt != nil {
		remaining := time.Until(*inst.ExpiresAt)
		if remaining <= 0 {
			e.markTimeout(ctx, inst, "saga timeout expired")
			return
		}
		var timeoutCancel context.CancelFunc
		runCtx, timeoutCancel = context.WithTimeout(runCtx, remaining)
		defer timeoutCancel()
	}

	for stepIdx := inst.CurrentStep; stepIdx < len(cfg.Steps); stepIdx++ {
		step := cfg.Steps[stepIdx]

		// Check context cancellation (timeout or explicit cancel)
		if err := runCtx.Err(); err != nil {
			if inst.ExpiresAt != nil && time.Now().After(*inst.ExpiresAt) {
				e.markTimeout(ctx, inst, "saga timeout")
			} else {
				inst.Status = StatusCancelled
				e.store.Save(ctx, inst)
			}
			return
		}

		// Handle delay step
		if step.Delay != "" {
			d, err := time.ParseDuration(step.Delay)
			if err != nil {
				inst.Status = StatusFailed
				inst.Error = fmt.Sprintf("invalid delay %q: %v", step.Delay, err)
				e.store.Save(ctx, inst)
				return
			}

			resumeAt := time.Now().Add(d)
			inst.ResumeAt = &resumeAt
			inst.CurrentStep = stepIdx + 1
			inst.Status = StatusPaused
			e.store.Save(ctx, inst)

			e.logger.Info("workflow paused for delay",
				slog.String("id", inst.ID),
				slog.String("delay", step.Delay),
			)
			return
		}

		// Handle await step
		if step.Await != "" {
			inst.AwaitEvent = step.Await
			inst.CurrentStep = stepIdx + 1
			inst.Status = StatusPaused

			// Step-level timeout for await
			if step.Timeout != "" {
				if d, err := time.ParseDuration(step.Timeout); err == nil {
					expires := time.Now().Add(d)
					inst.StepExpiresAt = &expires
				}
			}

			e.store.Save(ctx, inst)

			e.logger.Info("workflow awaiting event",
				slog.String("id", inst.ID),
				slog.String("event", step.Await),
			)
			return
		}

		// Normal action step — execute with optional step timeout
		stepCtx := runCtx
		if step.Timeout != "" {
			if d, err := time.ParseDuration(step.Timeout); err == nil {
				var stepCancel context.CancelFunc
				stepCtx, stepCancel = context.WithTimeout(runCtx, d)
				defer stepCancel()
			}
		}

		// Build execution context with input + step results for CEL
		execInput := make(map[string]interface{})
		for k, v := range inst.Input {
			execInput[k] = v
		}
		// Add signal data if available
		if inst.SignalData != nil {
			execInput["signal"] = inst.SignalData
		}

		result, err := e.executor.ExecuteStep(stepCtx, step, execInput, inst.StepResults)
		if err != nil {
			// Handle step error
			if step.OnError == "skip" {
				e.logger.Warn("workflow step failed, skipping",
					slog.String("id", inst.ID),
					slog.String("step", step.Name),
					slog.Any("error", err),
				)
				inst.CurrentStep = stepIdx + 1
				e.store.Save(ctx, inst)
				continue
			}

			// Run compensations for completed steps in reverse
			e.compensate(ctx, cfg, inst, stepIdx)

			inst.Status = StatusFailed
			inst.Error = fmt.Sprintf("step %q failed: %v", step.Name, err)
			e.store.Save(ctx, inst)

			// Run on_failure callback
			if cfg.OnFailure != nil {
				e.executor.ExecuteAction(ctx, cfg.OnFailure, execInput, inst.StepResults)
			}
			return
		}

		inst.StepResults[step.Name] = result
		inst.CurrentStep = stepIdx + 1
		e.store.Save(ctx, inst)
	}

	// All steps completed
	inst.Status = StatusCompleted
	inst.ResumeAt = nil
	inst.AwaitEvent = ""
	e.store.Save(ctx, inst)

	e.logger.Info("workflow completed",
		slog.String("id", inst.ID),
		slog.String("saga", inst.SagaName),
	)

	// Run on_complete callback
	if cfg.OnComplete != nil {
		execInput := make(map[string]interface{})
		for k, v := range inst.Input {
			execInput[k] = v
		}
		e.executor.ExecuteAction(ctx, cfg.OnComplete, execInput, inst.StepResults)
	}
}

// compensate runs compensation actions in reverse for completed steps.
func (e *Engine) compensate(ctx context.Context, cfg *saga.Config, inst *Instance, failedStep int) {
	execInput := make(map[string]interface{})
	for k, v := range inst.Input {
		execInput[k] = v
	}

	for i := failedStep - 1; i >= 0; i-- {
		step := cfg.Steps[i]
		if step.Compensate == nil {
			continue
		}
		// Skip steps that were delay/await (no action to compensate)
		if step.Delay != "" || step.Await != "" {
			continue
		}

		if _, err := e.executor.ExecuteAction(ctx, step.Compensate, execInput, inst.StepResults); err != nil {
			e.logger.Error("workflow compensation failed",
				slog.String("id", inst.ID),
				slog.String("step", step.Name),
				slog.Any("error", err),
			)
		}
	}
}

// markTimeout marks a workflow as timed out and runs compensation.
func (e *Engine) markTimeout(ctx context.Context, inst *Instance, reason string) {
	cfg, ok := e.sagas[inst.SagaName]
	if ok {
		e.compensate(ctx, cfg, inst, inst.CurrentStep)
	}

	inst.Status = StatusTimeout
	inst.Error = reason
	e.store.Save(ctx, inst)

	e.logger.Warn("workflow timed out",
		slog.String("id", inst.ID),
		slog.String("reason", reason),
	)
}

// resumeInstance resumes a running instance from its last checkpoint.
func (e *Engine) resumeInstance(ctx context.Context, inst *Instance) {
	e.logger.Info("resuming workflow",
		slog.String("id", inst.ID),
		slog.String("saga", inst.SagaName),
		slog.Int("step", inst.CurrentStep),
	)
	e.runFromStep(ctx, inst)
}

// tickLoop runs the background maintenance cycle.
func (e *Engine) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(e.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.processReady(ctx)
			e.processExpired(ctx)
		case <-e.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// processReady resumes paused instances whose delay has expired.
func (e *Engine) processReady(ctx context.Context) {
	ready, err := e.store.FindReady(ctx)
	if err != nil {
		e.logger.Error("failed to find ready workflows", slog.Any("error", err))
		return
	}

	for _, inst := range ready {
		inst.Status = StatusRunning
		inst.ResumeAt = nil
		e.store.Save(ctx, inst)
		go e.runFromStep(context.Background(), inst)
	}
}

// processExpired marks timed-out instances and runs compensation.
func (e *Engine) processExpired(ctx context.Context) {
	expired, err := e.store.FindExpired(ctx)
	if err != nil {
		e.logger.Error("failed to find expired workflows", slog.Any("error", err))
		return
	}

	for _, inst := range expired {
		// Cancel running context if any
		if cancel, ok := e.running.LoadAndDelete(inst.ID); ok {
			cancel.(context.CancelFunc)()
		}

		reason := "saga timeout"
		if inst.StepExpiresAt != nil && time.Now().After(*inst.StepExpiresAt) {
			reason = fmt.Sprintf("await step timeout (event: %s)", inst.AwaitEvent)
		}
		e.markTimeout(ctx, inst, reason)
	}
}

// NeedsPersistence checks if a saga has steps that require async execution.
func NeedsPersistence(config *saga.Config) bool {
	for _, step := range config.Steps {
		if step.Delay != "" || step.Await != "" {
			return true
		}
	}
	return false
}

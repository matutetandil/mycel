package debug

import (
	"sync"
	"sync/atomic"

	"github.com/matutetandil/mycel/internal/trace"
	"github.com/matutetandil/mycel/internal/transform"
)

// resumeAction controls what happens when a paused thread is resumed.
type resumeAction int

const (
	actionContinue resumeAction = iota
	actionNext
	actionStepInto
	actionAbort
)

// Session represents a connected debug client (IDE instance).
type Session struct {
	ID         string
	ClientName string

	mu          sync.Mutex
	breakpoints map[string][]BreakpointSpec // flow name → breakpoints
	threads     map[string]*DebugThread     // thread ID → thread
}

// NewSession creates a new debug session.
func NewSession(id, clientName string) *Session {
	return &Session{
		ID:          id,
		ClientName:  clientName,
		breakpoints: make(map[string][]BreakpointSpec),
		threads:     make(map[string]*DebugThread),
	}
}

// SetBreakpoints sets breakpoints for a specific flow.
func (s *Session) SetBreakpoints(flowName string, specs []BreakpointSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(specs) == 0 {
		delete(s.breakpoints, flowName)
	} else {
		s.breakpoints[flowName] = specs
	}
}

// GetBreakpoints returns breakpoints for a flow.
func (s *Session) GetBreakpoints(flowName string) []BreakpointSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.breakpoints[flowName]
}

// HasBreakpoints returns true if any breakpoints are set for the flow.
func (s *Session) HasBreakpoints(flowName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.breakpoints[flowName]) > 0
}

// AllBreakpointFlows returns all flows that have breakpoints set.
func (s *Session) AllBreakpointFlows() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	flows := make([]string, 0, len(s.breakpoints))
	for name := range s.breakpoints {
		flows = append(flows, name)
	}
	return flows
}

// RegisterThread adds a debug thread for a new request.
func (s *Session) RegisterThread(t *DebugThread) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[t.ID] = t
}

// UnregisterThread removes a completed debug thread.
func (s *Session) UnregisterThread(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.threads, id)
}

// GetThread returns a debug thread by ID.
func (s *Session) GetThread(id string) (*DebugThread, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[id]
	return t, ok
}

// ListThreads returns all active debug threads.
func (s *Session) ListThreads() []*DebugThread {
	s.mu.Lock()
	defer s.mu.Unlock()
	threads := make([]*DebugThread, 0, len(s.threads))
	for _, t := range s.threads {
		threads = append(threads, t)
	}
	return threads
}

// DebugThread represents a single request being debugged.
// Each concurrent request gets its own thread with independent pause/resume control.
type DebugThread struct {
	ID       string
	FlowName string

	// Channel-based pause/resume (same pattern as DAP)
	pauseCh  chan struct{}
	resumeCh chan resumeAction

	// Current state (protected by atomic/mutex)
	paused     atomic.Bool
	mu         sync.Mutex
	stage      trace.Stage
	name       string
	activation map[string]interface{} // CEL activation at breakpoint
	ruleInfo   *RuleInfo              // current rule info if paused inside transform

	// StepInto mode: when true, pause on each CEL rule
	stepInto atomic.Bool
}

// NewDebugThread creates a new debug thread.
func NewDebugThread(id, flowName string) *DebugThread {
	return &DebugThread{
		ID:       id,
		FlowName: flowName,
		pauseCh:  make(chan struct{}, 1),
		resumeCh: make(chan resumeAction, 1),
	}
}

// SetState updates the thread's current state.
func (t *DebugThread) SetState(stage trace.Stage, name string, activation map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stage = stage
	t.name = name
	t.activation = activation
}

// SetRuleInfo sets the current rule info when paused inside a transform.
func (t *DebugThread) SetRuleInfo(info *RuleInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ruleInfo = info
}

// GetState returns the current thread state.
func (t *DebugThread) GetState() (trace.Stage, string, map[string]interface{}, *RuleInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stage, t.name, t.activation, t.ruleInfo
}

// IsPaused returns whether the thread is paused.
func (t *DebugThread) IsPaused() bool {
	return t.paused.Load()
}

// Pause blocks execution at a breakpoint. Returns the resume action.
func (t *DebugThread) Pause() resumeAction {
	t.paused.Store(true)
	// Signal that we've paused
	select {
	case t.pauseCh <- struct{}{}:
	default:
	}
	// Wait for resume
	action := <-t.resumeCh
	t.paused.Store(false)
	return action
}

// Resume unblocks a paused thread with the given action.
func (t *DebugThread) Resume(action resumeAction) {
	select {
	case t.resumeCh <- action:
	default:
	}
}

// WaitForPause blocks until the thread is paused (used by test code).
func (t *DebugThread) WaitForPause() {
	<-t.pauseCh
}

// IsStepInto returns whether stepInto mode is active.
func (t *DebugThread) IsStepInto() bool {
	return t.stepInto.Load()
}

// SetStepInto enables or disables stepInto mode.
func (t *DebugThread) SetStepInto(v bool) {
	t.stepInto.Store(v)
}

// GetVariables returns variables at the current breakpoint.
func (t *DebugThread) GetVariables() *VariablesResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := &VariablesResult{}
	if t.activation != nil {
		result.Input = t.activation["input"]
		result.Output = t.activation["output"]
		result.Enriched = t.activation["enriched"]
		result.Steps = t.activation["step"]
	}
	result.Rule = t.ruleInfo
	return result
}

// EvaluateCEL evaluates a CEL expression in the thread's current activation context.
func (t *DebugThread) EvaluateCEL(transformer *transform.CELTransformer, expr string) (interface{}, error) {
	t.mu.Lock()
	activation := t.activation
	t.mu.Unlock()

	if activation == nil {
		activation = map[string]interface{}{
			"input":  map[string]interface{}{},
			"output": map[string]interface{}{},
		}
	}

	prog, err := transformer.Compile(expr)
	if err != nil {
		return nil, err
	}

	result, _, err := prog.Eval(activation)
	if err != nil {
		return nil, err
	}

	return result.Value(), nil
}

package dap

import (
	"encoding/json"
	"sync"

	"github.com/matutetandil/mycel/internal/trace"
)

// Session manages the state of a single debug session.
// It bridges between the DAP server and the trace system's Breakpoint controller.
type Session struct {
	mu sync.Mutex

	// flowName is the flow being debugged.
	flowName string

	// input is the flow input data.
	input map[string]interface{}

	// dryRun enables dry-run mode.
	dryRun bool

	// breakpointStages are the stages where the debugger should pause.
	// Empty means no breakpoints set (execution runs freely).
	breakpointStages map[trace.Stage]bool

	// currentStage is the stage where execution is currently paused.
	currentStage trace.Stage

	// currentName is the sub-name of the current stage.
	currentName string

	// currentData is the data snapshot at the current breakpoint.
	currentData interface{}

	// stageHistory tracks all stages seen so far (for stack trace).
	stageHistory []stageEntry

	// pauseCh is written to when execution should pause (sent by breakpoint).
	// The value is the stage info.
	pauseCh chan pauseInfo

	// resumeCh is written to when execution should resume (sent by DAP handler).
	// True = continue, false = abort.
	resumeCh chan resumeAction

	// doneCh is closed when the flow execution completes.
	doneCh chan struct{}

	// result holds the flow execution result.
	result interface{}

	// err holds any flow execution error.
	err error

	// launched indicates the flow has been launched.
	launched bool

	// finished indicates the flow has completed.
	finished bool
}

type stageEntry struct {
	stage trace.Stage
	name  string
	data  interface{}
}

type pauseInfo struct {
	stage trace.Stage
	name  string
	data  interface{}
}

type resumeAction int

const (
	actionContinue resumeAction = iota
	actionNext
	actionAbort
)

// NewSession creates a new debug session.
func NewSession() *Session {
	return &Session{
		breakpointStages: make(map[trace.Stage]bool),
		pauseCh:          make(chan pauseInfo, 1),
		resumeCh:         make(chan resumeAction, 1),
		doneCh:           make(chan struct{}),
	}
}

// SetBreakpoints sets the stages where the debugger should pause.
func (s *Session) SetBreakpoints(stages []trace.Stage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breakpointStages = make(map[trace.Stage]bool, len(stages))
	for _, st := range stages {
		s.breakpointStages[st] = true
	}
}

// GetBreakpointStages returns the current breakpoint stages.
func (s *Session) GetBreakpointStages() map[trace.Stage]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[trace.Stage]bool, len(s.breakpointStages))
	for k, v := range s.breakpointStages {
		result[k] = v
	}
	return result
}

// CurrentState returns the current breakpoint state.
func (s *Session) CurrentState() (trace.Stage, string, interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentStage, s.currentName, s.currentData
}

// StageHistory returns all stages seen so far.
func (s *Session) StageHistory() []stageEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]stageEntry, len(s.stageHistory))
	copy(result, s.stageHistory)
	return result
}

// VariablesForData converts data into DAP Variable list.
func VariablesForData(data interface{}) []Variable {
	if data == nil {
		return nil
	}

	switch v := data.(type) {
	case map[string]interface{}:
		vars := make([]Variable, 0, len(v))
		for key, val := range v {
			vars = append(vars, Variable{
				Name:  key,
				Value: FormatVariable(val),
				Type:  jsonType(val),
			})
		}
		return vars
	default:
		return []Variable{{
			Name:  "value",
			Value: FormatVariable(data),
			Type:  jsonType(data),
		}}
	}
}

func jsonType(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "boolean"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return "unknown"
	}
}

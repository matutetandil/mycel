package dap

import (
	"github.com/matutetandil/mycel/internal/trace"
)

// DAPBreakpoint implements the trace.Breakpoint interface but is controlled
// by the DAP server via channels instead of stdin.
//
// When execution hits a breakpoint, it sends stage info to the session's pauseCh
// and blocks until a resume action is received on resumeCh.
type DAPBreakpoint struct {
	session *Session
}

// NewDAPBreakpoint creates a breakpoint controller tied to a DAP session.
func NewDAPBreakpoint(session *Session) *DAPBreakpoint {
	return &DAPBreakpoint{session: session}
}

// ShouldBreak returns true if the current stage has a breakpoint set.
func (b *DAPBreakpoint) ShouldBreak(stage trace.Stage) bool {
	stages := b.session.GetBreakpointStages()
	return stages[stage]
}

// Pause blocks execution and notifies the DAP server that we hit a breakpoint.
// Returns false if the user wants to abort (disconnect).
func (b *DAPBreakpoint) Pause(stage trace.Stage, name string, data interface{}) bool {
	// Update session state
	b.session.mu.Lock()
	b.session.currentStage = stage
	b.session.currentName = name
	b.session.currentData = data
	b.session.stageHistory = append(b.session.stageHistory, stageEntry{
		stage: stage,
		name:  name,
		data:  data,
	})
	b.session.mu.Unlock()

	// Notify the DAP server that we've paused
	b.session.pauseCh <- pauseInfo{
		stage: stage,
		name:  name,
		data:  data,
	}

	// Wait for resume action from DAP server
	action := <-b.session.resumeCh

	return action != actionAbort
}

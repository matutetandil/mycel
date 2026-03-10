// Package dap implements the Debug Adapter Protocol (DAP) for Mycel flow debugging.
// DAP enables IDE integration (VS Code, IntelliJ, Neovim) for interactive flow debugging.
//
// Protocol spec: https://microsoft.github.io/debug-adapter-protocol/specification
//
// This implementation supports the minimum required for flow-level debugging:
// initialize, launch, setBreakpoints, threads, stackTrace, scopes, variables,
// continue, next, disconnect.
package dap

import (
	"encoding/json"
	"fmt"
)

// Message is the base DAP message.
type Message struct {
	Seq  int    `json:"seq"`
	Type string `json:"type"` // "request", "response", "event"
}

// Request is a DAP request from the client (IDE).
type Request struct {
	Message
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Response is a DAP response to a request.
type Response struct {
	Message
	RequestSeq int         `json:"request_seq"`
	Success    bool        `json:"success"`
	Command    string      `json:"command"`
	Body       interface{} `json:"body,omitempty"`
	ErrorMsg   string      `json:"message,omitempty"`
}

// Event is a DAP event sent to the client.
type Event struct {
	Message
	Event string      `json:"event"`
	Body  interface{} `json:"body,omitempty"`
}

// --- Request argument types ---

// InitializeArguments for the "initialize" request.
type InitializeArguments struct {
	ClientID   string `json:"clientID,omitempty"`
	ClientName string `json:"clientName,omitempty"`
}

// LaunchArguments for the "launch" request.
type LaunchArguments struct {
	Flow    string                 `json:"flow"`
	Input   map[string]interface{} `json:"input,omitempty"`
	DryRun  bool                   `json:"dryRun,omitempty"`
	Config  string                 `json:"config,omitempty"`
	BreakAt []string               `json:"breakAt,omitempty"`
}

// SetBreakpointsArguments for "setBreakpoints".
type SetBreakpointsArguments struct {
	Source      Source             `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints,omitempty"`
}

// Source identifies the flow being debugged.
type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// SourceBreakpoint is a breakpoint at a specific line/stage.
type SourceBreakpoint struct {
	Line int `json:"line"`
}

// ContinueArguments for "continue".
type ContinueArguments struct {
	ThreadID int `json:"threadId"`
}

// NextArguments for "next" (step over).
type NextArguments struct {
	ThreadID int `json:"threadId"`
}

// ThreadsResponseBody for "threads" response.
type ThreadsResponseBody struct {
	Threads []Thread `json:"threads"`
}

// Thread represents a debug thread (one per flow execution).
type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// StackTraceArguments for "stackTrace".
type StackTraceArguments struct {
	ThreadID int `json:"threadId"`
}

// StackTraceResponseBody for "stackTrace" response.
type StackTraceResponseBody struct {
	StackFrames []StackFrame `json:"stackFrames"`
	TotalFrames int          `json:"totalFrames"`
}

// StackFrame represents a pipeline stage in the call stack.
type StackFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source,omitempty"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// ScopesArguments for "scopes".
type ScopesArguments struct {
	FrameID int `json:"frameId"`
}

// ScopesResponseBody for "scopes" response.
type ScopesResponseBody struct {
	Scopes []Scope `json:"scopes"`
}

// Scope represents a variable scope (input data, output data, etc.).
type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

// VariablesArguments for "variables".
type VariablesArguments struct {
	VariablesReference int `json:"variablesReference"`
}

// VariablesResponseBody for "variables" response.
type VariablesResponseBody struct {
	Variables []Variable `json:"variables"`
}

// Variable represents a single variable in a scope.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// SetBreakpointsResponseBody for "setBreakpoints" response.
type SetBreakpointsResponseBody struct {
	Breakpoints []ResponseBreakpoint `json:"breakpoints"`
}

// ResponseBreakpoint is a verified breakpoint.
type ResponseBreakpoint struct {
	ID       int    `json:"id"`
	Verified bool   `json:"verified"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message,omitempty"`
}

// --- Event bodies ---

// StoppedEventBody for "stopped" event.
type StoppedEventBody struct {
	Reason   string `json:"reason"` // "breakpoint", "step", "entry"
	ThreadID int    `json:"threadId"`
	Text     string `json:"text,omitempty"`
}

// TerminatedEventBody for "terminated" event.
type TerminatedEventBody struct{}

// OutputEventBody for "output" event.
type OutputEventBody struct {
	Category string `json:"category,omitempty"` // "console", "stdout", "stderr"
	Output   string `json:"output"`
}

// Capabilities describes the debug adapter capabilities.
type Capabilities struct {
	SupportsConfigurationDoneRequest bool `json:"supportsConfigurationDoneRequest"`
}

// --- Pipeline stage to line mapping ---
// DAP uses line numbers for breakpoints. We map pipeline stages to virtual line numbers.

// StageLines maps pipeline stages to virtual line numbers for DAP breakpoints.
var StageLines = map[string]int{
	"input":           1,
	"sanitize":        2,
	"filter":          3,
	"dedupe":          4,
	"validate_input":  5,
	"enrich":          6,
	"transform":       7,
	"step":            8,
	"validate_output": 9,
	"read":            10,
	"write":           11,
}

// LineToStage maps virtual line numbers back to stage names.
var LineToStage = func() map[int]string {
	m := make(map[int]string, len(StageLines))
	for stage, line := range StageLines {
		m[line] = stage
	}
	return m
}()

// FormatVariable converts a value to a display string for DAP.
func FormatVariable(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("%q", val)
	case map[string]interface{}:
		b, _ := json.Marshal(val)
		return string(b)
	case []interface{}:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}

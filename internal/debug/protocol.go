// Package debug implements the Mycel Studio Debug Protocol.
// It provides a WebSocket JSON-RPC 2.0 server for real-time debugging
// of Mycel flows, including breakpoints, variable inspection, CEL
// expression evaluation, and pipeline event streaming.
package debug

import (
	"encoding/json"

	"github.com/matutetandil/mycel/internal/trace"
)

// JSON-RPC 2.0 types

// Request is a JSON-RPC 2.0 request from the IDE.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response to the IDE.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no ID, no response expected).
type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// Custom error codes
	CodeSessionNotFound = -32000
	CodeThreadNotFound  = -32001
	CodeFlowNotFound    = -32002
	CodeEvalError       = -32003
)

// --- Request params ---

// AttachParams is sent by the IDE to establish a debug session.
type AttachParams struct {
	// ClientName identifies the IDE (e.g., "mycel-studio 1.0").
	ClientName string `json:"clientName,omitempty"`
}

// AttachResult is returned after a successful attach.
type AttachResult struct {
	SessionID string   `json:"sessionId"`
	Flows     []string `json:"flows"`
}

// SetBreakpointsParams configures breakpoints for a flow.
type SetBreakpointsParams struct {
	Flow        string           `json:"flow"`
	Breakpoints []BreakpointSpec `json:"breakpoints"`
}

// BreakpointSpec defines a single breakpoint.
type BreakpointSpec struct {
	// Stage is the pipeline stage (e.g., "transform", "validate_input").
	Stage trace.Stage `json:"stage"`

	// RuleIndex targets a specific CEL rule within a transform stage.
	// -1 means break on the entire stage.
	RuleIndex int `json:"ruleIndex"`

	// Condition is an optional CEL expression that must evaluate to true
	// for the breakpoint to trigger.
	Condition string `json:"condition,omitempty"`
}

// SetBreakpointsResult confirms which breakpoints were set.
type SetBreakpointsResult struct {
	Breakpoints []BreakpointSpec `json:"breakpoints"`
}

// ContinueParams resumes a paused thread.
type ContinueParams struct {
	ThreadID string `json:"threadId"`
}

// NextParams steps to the next stage.
type NextParams struct {
	ThreadID string `json:"threadId"`
}

// StepIntoParams steps per-CEL-rule within a transform stage.
type StepIntoParams struct {
	ThreadID string `json:"threadId"`
}

// EvaluateParams evaluates a CEL expression in the current context.
type EvaluateParams struct {
	ThreadID   string `json:"threadId"`
	Expression string `json:"expression"`
}

// EvaluateResult returns the evaluation result.
type EvaluateResult struct {
	Result interface{} `json:"result"`
	Type   string      `json:"type"`
}

// VariablesParams requests variables at the current breakpoint.
type VariablesParams struct {
	ThreadID string `json:"threadId"`
}

// VariablesResult returns the variables at a breakpoint.
type VariablesResult struct {
	Input    interface{} `json:"input,omitempty"`
	Output   interface{} `json:"output,omitempty"`
	Enriched interface{} `json:"enriched,omitempty"`
	Steps    interface{} `json:"steps,omitempty"`
	Rule     *RuleInfo   `json:"rule,omitempty"`
}

// RuleInfo describes the current CEL rule being evaluated.
type RuleInfo struct {
	Index      int         `json:"index"`
	Target     string      `json:"target"`
	Expression string      `json:"expression"`
	Result     interface{} `json:"result,omitempty"`
}

// ThreadsResult lists active debug threads.
type ThreadsResult struct {
	Threads []ThreadInfo `json:"threads"`
}

// ThreadInfo describes an active debug thread.
type ThreadInfo struct {
	ID       string      `json:"id"`
	FlowName string      `json:"flowName"`
	Stage    trace.Stage `json:"stage"`
	Name     string      `json:"name,omitempty"`
	Paused   bool        `json:"paused"`
}

// --- Inspect params ---

// InspectFlowParams requests detailed flow configuration.
type InspectFlowParams struct {
	Name string `json:"name"`
}

// FlowInfo describes a flow for the IDE.
type FlowInfo struct {
	Name       string            `json:"name"`
	From       *FlowEndpoint     `json:"from,omitempty"`
	To         *FlowEndpoint     `json:"to,omitempty"`
	HasSteps   bool              `json:"hasSteps"`
	StepCount  int               `json:"stepCount"`
	Transform  map[string]string `json:"transform,omitempty"`
	Response   map[string]string `json:"response,omitempty"`
	Validate   *ValidateInfo     `json:"validate,omitempty"`
	HasCache   bool              `json:"hasCache"`
	HasRetry   bool              `json:"hasRetry"`
}

// FlowEndpoint describes a flow source or destination.
type FlowEndpoint struct {
	Connector string `json:"connector"`
	Operation string `json:"operation,omitempty"`
}

// ValidateInfo describes flow validation config.
type ValidateInfo struct {
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
}

// ConnectorInfo describes a connector for the IDE.
type ConnectorInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Driver string `json:"driver,omitempty"`
}

// TypeInfo describes a type schema for the IDE.
type TypeInfo struct {
	Name   string      `json:"name"`
	Fields []FieldInfo `json:"fields"`
}

// FieldInfo describes a type field.
type FieldInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// TransformInfo describes a named transform for the IDE.
type TransformInfo struct {
	Name     string            `json:"name"`
	Mappings map[string]string `json:"mappings"`
}

// --- Event params (runtime → IDE) ---

// StoppedEvent is sent when a thread hits a breakpoint.
type StoppedEvent struct {
	ThreadID string      `json:"threadId"`
	FlowName string      `json:"flowName"`
	Stage    trace.Stage `json:"stage"`
	Name     string      `json:"name,omitempty"`
	Rule     *RuleInfo   `json:"rule,omitempty"`
	Reason   string      `json:"reason"` // "breakpoint", "step", "stepInto"
}

// ContinuedEvent is sent when a thread resumes.
type ContinuedEvent struct {
	ThreadID string `json:"threadId"`
}

// StageEnterEvent is sent when a pipeline stage starts.
type StageEnterEvent struct {
	ThreadID string      `json:"threadId"`
	FlowName string      `json:"flowName"`
	Stage    trace.Stage `json:"stage"`
	Name     string      `json:"name,omitempty"`
	Input    interface{} `json:"input,omitempty"`
}

// StageExitEvent is sent when a pipeline stage completes.
type StageExitEvent struct {
	ThreadID string      `json:"threadId"`
	FlowName string      `json:"flowName"`
	Stage    trace.Stage `json:"stage"`
	Name     string      `json:"name,omitempty"`
	Output   interface{} `json:"output,omitempty"`
	Duration int64       `json:"durationUs"` // microseconds
	Error    string      `json:"error,omitempty"`
}

// RuleEvalEvent is sent when an individual CEL rule is evaluated.
type RuleEvalEvent struct {
	ThreadID   string      `json:"threadId"`
	FlowName   string      `json:"flowName"`
	Stage      trace.Stage `json:"stage"`
	RuleIndex  int         `json:"ruleIndex"`
	Target     string      `json:"target"`
	Expression string      `json:"expression"`
	Result     interface{} `json:"result,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// FlowStartEvent is sent when a request enters a flow.
type FlowStartEvent struct {
	ThreadID string      `json:"threadId"`
	FlowName string      `json:"flowName"`
	Input    interface{} `json:"input,omitempty"`
}

// FlowEndEvent is sent when a request completes a flow.
type FlowEndEvent struct {
	ThreadID string      `json:"threadId"`
	FlowName string      `json:"flowName"`
	Output   interface{} `json:"output,omitempty"`
	Duration int64       `json:"durationUs"`
	Error    string      `json:"error,omitempty"`
}

// newResponse creates a successful JSON-RPC response.
func newResponse(id json.RawMessage, result interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// newErrorResponse creates an error JSON-RPC response.
func newErrorResponse(id json.RawMessage, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

// newNotification creates a JSON-RPC notification.
func newNotification(method string, params interface{}) *Notification {
	return &Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
}

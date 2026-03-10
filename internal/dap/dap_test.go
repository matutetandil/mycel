package dap

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/trace"
)

func TestSessionSetBreakpoints(t *testing.T) {
	s := NewSession()
	s.SetBreakpoints([]trace.Stage{trace.StageTransform, trace.StageWrite})

	stages := s.GetBreakpointStages()
	if !stages[trace.StageTransform] {
		t.Error("expected transform breakpoint")
	}
	if !stages[trace.StageWrite] {
		t.Error("expected write breakpoint")
	}
	if stages[trace.StageInput] {
		t.Error("should not have input breakpoint")
	}
}

func TestDAPBreakpointShouldBreak(t *testing.T) {
	s := NewSession()
	s.SetBreakpoints([]trace.Stage{trace.StageWrite})
	bp := NewDAPBreakpoint(s)

	if bp.ShouldBreak(trace.StageInput) {
		t.Error("should not break at input")
	}
	if !bp.ShouldBreak(trace.StageWrite) {
		t.Error("should break at write")
	}
}

func TestDAPBreakpointPause(t *testing.T) {
	s := NewSession()
	s.SetBreakpoints([]trace.Stage{trace.StageTransform})
	bp := NewDAPBreakpoint(s)

	// Simulate: resume after a short delay
	go func() {
		// Wait for pause notification
		<-s.pauseCh
		// Resume
		s.resumeCh <- actionContinue
	}()

	result := bp.Pause(trace.StageTransform, "test_step", map[string]interface{}{"x": 1})
	if !result {
		t.Error("expected continue (true)")
	}

	stage, name, _ := s.CurrentState()
	if stage != trace.StageTransform {
		t.Errorf("current stage = %q, want transform", stage)
	}
	if name != "test_step" {
		t.Errorf("current name = %q, want test_step", name)
	}
}

func TestDAPBreakpointAbort(t *testing.T) {
	s := NewSession()
	s.SetBreakpoints([]trace.Stage{trace.StageWrite})
	bp := NewDAPBreakpoint(s)

	go func() {
		<-s.pauseCh
		s.resumeCh <- actionAbort
	}()

	result := bp.Pause(trace.StageWrite, "", nil)
	if result {
		t.Error("expected abort (false)")
	}
}

func TestVariablesForData(t *testing.T) {
	data := map[string]interface{}{
		"email": "test@example.com",
		"age":   float64(25),
		"admin": true,
	}

	vars := VariablesForData(data)
	if len(vars) != 3 {
		t.Fatalf("expected 3 variables, got %d", len(vars))
	}

	// Check that all keys are present
	found := make(map[string]bool)
	for _, v := range vars {
		found[v.Name] = true
	}
	for _, key := range []string{"email", "age", "admin"} {
		if !found[key] {
			t.Errorf("missing variable %q", key)
		}
	}
}

func TestVariablesForDataNil(t *testing.T) {
	vars := VariablesForData(nil)
	if vars != nil {
		t.Errorf("expected nil, got %v", vars)
	}
}

func TestStageLineMapping(t *testing.T) {
	// Verify all stages have a line
	stages := []string{"input", "sanitize", "filter", "dedupe", "validate_input",
		"enrich", "transform", "step", "validate_output", "read", "write"}

	for _, stage := range stages {
		line, ok := StageLines[stage]
		if !ok {
			t.Errorf("stage %q has no line mapping", stage)
		}
		// Verify reverse mapping
		name, ok := LineToStage[line]
		if !ok || name != stage {
			t.Errorf("line %d does not map back to %q", line, stage)
		}
	}
}

func TestFormatVariable(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{nil, "null"},
		{"hello", `"hello"`},
		{float64(42), "42"},
		{map[string]interface{}{"a": 1}, `{"a":1}`},
	}

	for _, tt := range tests {
		got := FormatVariable(tt.input)
		if got != tt.expected {
			t.Errorf("FormatVariable(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestDAPProtocol verifies the DAP server handles the core protocol flow.
func TestDAPProtocol(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(0, logger) // port 0 = random

	launched := make(chan bool, 1)
	server.OnLaunch(func(args LaunchArguments) error {
		launched <- true
		if args.Flow != "test_flow" {
			t.Errorf("expected flow 'test_flow', got %q", args.Flow)
		}
		// Simulate flow completion
		time.Sleep(50 * time.Millisecond)
		server.NotifyFlowDone(map[string]interface{}{"ok": true}, nil)
		return nil
	})

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Get the actual port
	addr := server.listener.Addr().(*net.TCPAddr)

	// Connect as IDE client
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", addr.Port))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Send initialize
	sendDAP(t, conn, 1, "initialize", nil)
	resp := readDAP(t, reader)
	if !resp.Success {
		t.Error("initialize should succeed")
	}

	// Read initialized event
	readDAPRaw(t, reader) // initialized event

	// Send setBreakpoints
	sendDAP(t, conn, 2, "setBreakpoints", &SetBreakpointsArguments{
		Source:      Source{Name: "test_flow"},
		Breakpoints: []SourceBreakpoint{{Line: 7}}, // transform
	})
	bpResp := readDAP(t, reader)
	if !bpResp.Success {
		t.Error("setBreakpoints should succeed")
	}

	// Verify breakpoint was set
	stages := server.Session().GetBreakpointStages()
	if !stages[trace.StageTransform] {
		t.Error("expected transform breakpoint to be set")
	}

	// Send configurationDone
	sendDAP(t, conn, 3, "configurationDone", nil)
	readDAP(t, reader) // configurationDone response

	// Send launch
	sendDAP(t, conn, 4, "launch", &LaunchArguments{
		Flow:  "test_flow",
		Input: map[string]interface{}{"x": 1},
	})
	readDAP(t, reader) // launch response

	// Wait for launch callback
	select {
	case <-launched:
	case <-time.After(2 * time.Second):
		t.Fatal("launch callback not called")
	}

	// Send threads
	sendDAP(t, conn, 5, "threads", nil)
	threadsResp := readDAP(t, reader)
	if !threadsResp.Success {
		t.Error("threads should succeed")
	}

	// Send disconnect
	sendDAP(t, conn, 6, "disconnect", nil)
	readDAP(t, reader) // disconnect response

	// Server should exit
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Error("server should have stopped")
	}
}

// Helper: send a DAP request
func sendDAP(t *testing.T, conn net.Conn, seq int, command string, args interface{}) {
	t.Helper()
	var rawArgs json.RawMessage
	if args != nil {
		b, _ := json.Marshal(args)
		rawArgs = b
	}
	req := Request{
		Message:   Message{Seq: seq, Type: "request"},
		Command:   command,
		Arguments: rawArgs,
	}
	data, _ := json.Marshal(req)
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	conn.Write([]byte(header))
	conn.Write(data)
}

// Helper: read a DAP response
func readDAP(t *testing.T, reader *bufio.Reader) Response {
	t.Helper()
	raw := readDAPRaw(t, reader)
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("failed to parse response: %v\nraw: %s", err, string(raw))
	}
	return resp
}

// Helper: read a raw DAP message
func readDAPRaw(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	msg, err := readMessage(reader)
	if err != nil {
		t.Fatalf("failed to read DAP message: %v", err)
	}
	return msg
}

// TestReadMessage verifies Content-Length framing.
func TestReadMessage(t *testing.T) {
	body := `{"seq":1,"type":"request","command":"initialize"}`
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	reader := bufio.NewReader(strings.NewReader(input))

	msg, err := readMessage(reader)
	if err != nil {
		t.Fatal(err)
	}

	var req Request
	if err := json.Unmarshal(msg, &req); err != nil {
		t.Fatal(err)
	}
	if req.Command != "initialize" {
		t.Errorf("command = %q, want 'initialize'", req.Command)
	}
}

// TestWriteMessage verifies Content-Length framing on write.
func TestWriteMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(0, logger)

	// Use a pipe to capture output
	pr, pw := io.Pipe()
	server.connMu.Lock()
	server.conn = &pipeConn{pw}
	server.connMu.Unlock()

	go func() {
		server.sendEvent("stopped", &StoppedEventBody{
			Reason:   "breakpoint",
			ThreadID: 1,
		})
		pw.Close()
	}()

	var buf bytes.Buffer
	io.Copy(&buf, pr)

	output := buf.String()
	if !strings.HasPrefix(output, "Content-Length:") {
		t.Error("expected Content-Length header")
	}
	if !strings.Contains(output, `"event":"stopped"`) {
		t.Error("expected stopped event")
	}
}

// pipeConn wraps an io.WriteCloser to satisfy net.Conn for testing.
type pipeConn struct {
	w io.WriteCloser
}

func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeConn) Read([]byte) (int, error)     { return 0, io.EOF }
func (p *pipeConn) Close() error                 { return p.w.Close() }
func (p *pipeConn) LocalAddr() net.Addr          { return nil }
func (p *pipeConn) RemoteAddr() net.Addr         { return nil }
func (p *pipeConn) SetDeadline(time.Time) error  { return nil }
func (p *pipeConn) SetReadDeadline(time.Time) error  { return nil }
func (p *pipeConn) SetWriteDeadline(time.Time) error { return nil }
